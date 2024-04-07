package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/eclipse/paho.golang/paho"
	"github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// set up a MQTT broker for testing.
	logger := debugLogger(os.Stderr)
	logger.Info("starting broker")
	broker, err := makeBroker(logger)

	if err != nil {
		logger.Error("makeBroker", "error", err)
		os.Exit(1)
	}
	ret := m.Run()
	err = broker.Close()
	if err != nil {
		logger.Error("broker.Close", "error", err)
		os.Exit(1)
	}
	os.Exit(ret)
}

func Test_Complete(t *testing.T) {
	logger := debugLogger(os.Stderr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := runEchoer(ctx, logger)
		if err != nil {
			t.Errorf("runEchoer: %v", err)
			return
		}
	}()

	// set up buffers for the input and outputs
	out := bytes.NewBuffer(nil)
	logs := bytes.NewBuffer(nil)
	in := bytes.NewBufferString("ping")

	// then run the application
	err := run(ctx, logs, out, in, []string{"-server", "localhost:1881", "-topic", "foo", "-response-topic", "foo/resp", "-clientID", "client-1", "-timeout", "60s"}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != "pong" {
		t.Fatalf("unexpected output: %s", out.String())
	}

}

// runEchoer is a simple echoer that listens for a message on the topic "foo" and responds with "pong" on the response topic.
// The response topic is taken from the incoming message's properties.
func runEchoer(ctx context.Context, logger *slog.Logger) error {
	config := config{
		server:        "localhost:1881",
		responseTopic: "foo",
		qos:           1,
		clientID:      fmt.Sprintf("echoer-%d", rand.IntN(1000)),
	}
	client, msgChan, err := connect(ctx, config, slog.Default())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	select {
	case <-ctx.Done():
		logger.Info("echoer context done")
		return nil
	case pkt := <-msgChan:
		logger.Info("received message", "topic", pkt.Topic, "payload", string(pkt.Payload))
		if pkt.Topic != "foo" {
			return fmt.Errorf("unexpected topic: %s", pkt.Topic)
		}
		if string(pkt.Payload) != "ping" {
			return fmt.Errorf("unexpected payload: %s", pkt.Payload)
		}
		// send a response - "pong" on the response topic
		_, err = client.Publish(ctx, &paho.Publish{
			Topic:   pkt.Properties.ResponseTopic,
			Payload: []byte("pong"),
			Properties: &paho.PublishProperties{
				CorrelationData: pkt.Properties.CorrelationData,
			},
		})
	}
	return nil
}

func makeBroker(logger *slog.Logger) (*mqtt.Server, error) {
	mqttLogger := logger.With("module", "mqtt")
	server := mqtt.New(&mqtt.Options{Logger: mqttLogger})
	// Allow all connections.
	_ = server.AddHook(new(auth.AllowHook), nil)
	// Log all the stuff
	_ = server.AddHook(new(LogHook), nil)

	// Create a TCP listener on a standard port.
	tcp := listeners.NewTCP(listeners.Config{
		Type:      "tcp",
		ID:        "t1",
		Address:   ":1881",
		TLSConfig: nil,
	})
	err := server.AddListener(tcp)
	if err != nil {
		return nil, fmt.Errorf("server.AddListener: %w", err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			logger.Error("server.Serve", "error", err)
		} else {
			logger.Info("server.Serve", "message", "server stopped")
		}
	}()
	logger.Info("broker started")
	return server, nil
}

// testConnect is almost the same as connect but subscribes to "topic" instead of "responseTopic"
func testConnect(ctx context.Context, config *config, logger *slog.Logger) (*paho.Client, chan *paho.Publish, error) {
	conn, err := net.Dial("tcp", config.server)
	if err != nil {
		return nil, nil, fmt.Errorf("net.Dial: %w", err)
	}
	msgChan := make(chan *paho.Publish, 5)

	client := paho.NewClient(paho.ClientConfig{
		Conn: conn,
		OnPublishReceived: []func(paho.PublishReceived) (bool, error){ // Noop handler
			func(pr paho.PublishReceived) (bool, error) {
				msgChan <- pr.Packet
				return true, nil
			}},
		OnClientError: func(err error) {
			logger.Error("client error", "error", err)
		},
	})

	connectPacket := &paho.Connect{
		KeepAlive:  30,
		ClientID:   config.clientID,
		CleanStart: true,
		Username:   config.username,
		Password:   []byte(config.password),
	}

	if config.username != "" {
		connectPacket.UsernameFlag = true
	}
	if config.password != "" {
		connectPacket.PasswordFlag = true
	}

	logger.Debug("connecting to server", "server", config.server, "clientID", config.clientID)

	connAck, err := client.Connect(ctx, connectPacket)
	if err != nil {
		return nil, nil, fmt.Errorf("client.Connect: %w", err)
	}
	if connAck.ReasonCode != 0 {
		return nil, nil, fmt.Errorf("connection refused: (code: %d) %s", connAck.ReasonCode, connAck.Properties.ReasonString)
	}
	logger.Debug("connected to server")
	// subscribe to the response topic.
	subPacket := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{
				Topic: config.topic,
				QoS:   byte(config.qos),
			},
		},
	}
	subAck, err := client.Subscribe(ctx, subPacket)
	if err != nil {
		return nil, nil, fmt.Errorf("client.Subscribe: %w", err)
	}
	// not sure we need to check the subAck, but we can log it
	logger.Debug("subscribed to response topic", "topic", config.responseTopic, "qos", config.qos, "suback", subAck)

	return client, msgChan, nil
}

func debugLogger(output io.WriteCloser) *slog.Logger {
	logHandle := slog.NewTextHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(logHandle)

}
