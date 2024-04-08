package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"github.com/eclipse/paho.golang/paho"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	correlationDataSize = 10
	defaultTimeout      = 10 * time.Second
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	err := run(ctx, os.Stderr, os.Stdout, os.Stdin, os.Args[1:], os.Environ())
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

type config struct {
	server      string
	mdnsName    string
	publishTo   string
	subscribeTo string
	qos         int
	retained    bool
	clientID    string
	username    string
	password    string
	timeout     time.Duration
}

func run(ctx context.Context, logoutput, output io.Writer, input io.Reader, args, env []string) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	logHandle := slog.NewTextHandler(logoutput, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(logHandle)
	config, showhelp, err := getConfig(args)
	if err != nil {
		return fmt.Errorf("getConfig: %w", err)
	}
	if showhelp {
		return nil
	}
	fmt.Printf("config: %+v", config)
	logger.Debug("config", "config", config)
	client, msgChan, err := connect(runCtx, config, logger)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	go func() {
		<-runCtx.Done()
		logger.Info("context cancelled, disconnecting client")
		if client != nil {
			d := &paho.Disconnect{ReasonCode: 0}
			_ = client.Disconnect(d)
		}

		// Just assume the rest of the application is able to shut down
	}()

	// read the input until OEF
	payload, err := read(ctx, input)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	// make a random client id
	randomID, err := randomBytes(correlationDataSize)
	if err != nil {
		return fmt.Errorf("randomBytes: %w", err)
	}

	// wait for the response
	ctxTimeout, timeoutCancel := context.WithTimeout(runCtx, config.timeout)
	defer timeoutCancel()
	pubPacket := makePubPacket(config, payload, randomID)
	// publish the message
	resp, err := client.Publish(ctx, pubPacket)
	if err != nil {
		return fmt.Errorf("client.Publish: %w", err)
	}
	if resp.ReasonCode != 0 {
		return fmt.Errorf("publish refused: (code: %d) %s", resp.ReasonCode, resp.Properties.ReasonString)
	}

	// wait for the response or timeout
	for {
		select {
		case <-ctxTimeout.Done():
			return fmt.Errorf("timeout waiting for response")
		case msg := <-msgChan:
			logger.Info("received message", "topic", msg.Topic, "payload", string(msg.Payload),
				"correlationData", b64(msg.Properties.CorrelationData),
				"randomID", b64(randomID))
			if bytes.Equal(msg.Properties.CorrelationData, randomID) {
				_, err := output.Write(msg.Payload)
				if err != nil {
					return fmt.Errorf("output.Write: %w", err)
				}
				return nil
			}
		}
	}
}

func b64(data []byte) string {
	return fmt.Sprintf("%x", data)
}

func connect(ctx context.Context, config config, logger *slog.Logger) (*paho.Client, chan *paho.Publish, error) {
	//
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
	// return early if we don't need to subscribe to anything
	if config.subscribeTo == "" {
		return client, msgChan, nil
	}
	// subscribe to the response topic.
	subPacket := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{
				Topic: config.subscribeTo,
				QoS:   byte(config.qos),
			},
		},
	}
	subAck, err := client.Subscribe(ctx, subPacket)
	if err != nil {
		return nil, nil, fmt.Errorf("client.Subscribe: %w", err)
	}
	// not sure we need to check the subAck, but we can log it
	logger.Debug("subscribed to response topic", "topic", config.subscribeTo, "qos", config.qos, "suback", subAck)

	return client, msgChan, nil
}

func getConfig(args []string) (config, bool, error) {
	clientID, err := generateClientID()
	if err != nil {
		return config{}, false, fmt.Errorf("generateClientID: %w", err)
	}
	var cfg config
	var help bool
	flagset := flag.NewFlagSet("mqtt-pub", flag.ContinueOnError)
	flagset.StringVar(&cfg.server, "server", "localhost:1883", "The full URL of the MQTT server to connect to")
	flagset.StringVar(&cfg.mdnsName, "mdns", "", "The mDNS name of the MQTT server to connect to")
	flagset.StringVar(&cfg.publishTo, "topic", "", "Topic to publish the messages on")
	flagset.StringVar(&cfg.subscribeTo, "response-topic", "", "Topic the other party should respond to. Defaults to the publish topic")
	flagset.IntVar(&cfg.qos, "qos", 1, "The QoS to send the messages at")
	flagset.BoolVar(&cfg.retained, "retained", false, "Are the messages sent with the retained flag")
	flagset.StringVar(&cfg.clientID, "clientID", clientID, "A clientID for the connection")
	flagset.StringVar(&cfg.username, "username", "", "A username to authenticate to the MQTT server")
	flagset.StringVar(&cfg.password, "password", "", "Password to match username")
	flagset.DurationVar(&cfg.timeout, "timeout", defaultTimeout, "Timeout for the operation")
	flagset.BoolVar(&help, "help", false, "Print this help message")
	err = flagset.Parse(args)
	if err != nil {
		return config{}, false, fmt.Errorf("flagset.Parse: %w", err)
	}
	if help {
		flagset.Usage()
		return config{}, true, nil

	}
	// check for required flags:
	if cfg.publishTo == "" {
		return config{}, false, fmt.Errorf("missing required flag: -topic")
	}
	if cfg.timeout <= 0 {
		return config{}, false, fmt.Errorf("timeout must be positive")
	}
	if cfg.subscribeTo == "" {
		cfg.subscribeTo = cfg.publishTo
	}
	return cfg, false, nil
}

func makePubPacket(config config, payload, id []byte) *paho.Publish {
	// construct the publish packet
	return &paho.Publish{
		Topic:   config.publishTo,
		Payload: payload,
		QoS:     byte(config.qos),
		Retain:  config.retained,
		Properties: &paho.PublishProperties{
			CorrelationData:        id,
			ContentType:            "",
			ResponseTopic:          config.subscribeTo,
			PayloadFormat:          nil,
			MessageExpiry:          nil,
			SubscriptionIdentifier: nil,
			TopicAlias:             nil,
			User:                   nil,
		},
	}

}

func generateClientID() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("os.Hostname: %w", err)
	}
	// strip the domain name
	parts := strings.Split(hostname, ".")
	hostname = parts[0]
	return fmt.Sprintf("whippet-%s-%d", hostname, mathrand.Uint32()), nil
}

func read(ctx context.Context, input io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	done := make(chan error, 1)

	go func() {
		_, err := io.Copy(&buf, input)
		done <- err
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("io.Copy: %w", err)
		}
		return buf.Bytes(), nil
	}
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, fmt.Errorf("rand.Read: %w", err)
	}
	return b, nil
}
