package whippet

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"github.com/eclipse/paho.golang/paho"
	"log/slog"
	mathrand "math/rand/v2"
	"net"
	"os"
	"strings"
	"time"
)

const (
	defaultTimeout      = 10 * time.Second
	correlationDataSize = 10
)

type Config struct {
	Server      string
	PublishTo   string
	SubscribeTo string
	Qos         int
	Retained    bool
	ClientID    string
	Username    string
	Password    string
	Timeout     time.Duration
}

func Request(ctx context.Context, client *paho.Client, config Config, payload []byte, msgCh chan *paho.Publish, logger *slog.Logger) ([]byte, error) {

	// make a random client id
	randomID, err := makeRandomID()
	if err != nil {
		return nil, fmt.Errorf("makeRandomID: %w", err)
	}

	// wait for the response
	ctxTimeout, timeoutCancel := context.WithTimeout(ctx, config.Timeout)
	defer timeoutCancel()
	pubPacket := makePubPacket(config, payload, randomID)
	// publish the message
	resp, err := client.Publish(ctx, pubPacket)
	if err != nil {
		return nil, fmt.Errorf("client.Publish: %w", err)
	}
	if resp.ReasonCode != 0 {
		return nil, fmt.Errorf("publish refused: (code: %d) %s", resp.ReasonCode, resp.Properties.ReasonString)
	}

	// wait for the response or timeout
	for {
		select {
		case <-ctxTimeout.Done():
			return nil, fmt.Errorf("timeout waiting for response")
		case msg := <-msgCh:
			logger.Info("received message", "topic", msg.Topic, "payload", string(msg.Payload),
				"correlationData", b64(msg.Properties.CorrelationData),
				"randomID", b64(randomID))
			if bytes.Equal(msg.Properties.CorrelationData, randomID) {
				return msg.Payload, nil
			}
		}
	}
}

func Connect(ctx context.Context, config Config, logger *slog.Logger) (*paho.Client, chan *paho.Publish, error) {
	//
	conn, err := net.Dial("tcp", config.Server)
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
		ClientID:   config.ClientID,
		CleanStart: true,
		Username:   config.Username,
		Password:   []byte(config.Password),
	}

	if config.Username != "" {
		connectPacket.UsernameFlag = true
	}
	if config.Password != "" {
		connectPacket.PasswordFlag = true
	}

	logger.Debug("connecting to Server", "Server", config.Server, "ClientID", config.ClientID)

	connAck, err := client.Connect(ctx, connectPacket)
	if err != nil {
		return nil, nil, fmt.Errorf("client.Connect: %w", err)
	}
	if connAck.ReasonCode != 0 {
		return nil, nil, fmt.Errorf("connection refused: (code: %d) %s", connAck.ReasonCode, connAck.Properties.ReasonString)
	}
	logger.Debug("connected to Server")
	// return early if we don't need to subscribe to anything
	if config.SubscribeTo == "" {
		return client, msgChan, nil
	}
	// subscribe to the response topic.
	subPacket := &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{
				Topic: config.SubscribeTo,
				QoS:   byte(config.Qos),
			},
		},
	}
	subAck, err := client.Subscribe(ctx, subPacket)
	if err != nil {
		return nil, nil, fmt.Errorf("client.Subscribe: %w", err)
	}
	// not sure we need to check the subAck, but we can log it
	logger.Debug("subscribed to response topic", "topic", config.SubscribeTo, "Qos", config.Qos, "suback", subAck)

	return client, msgChan, nil
}

func GetConfig(args []string) (Config, bool, error) {
	clientID, err := generateClientID()
	if err != nil {
		return Config{}, false, fmt.Errorf("generateClientID: %w", err)
	}
	var cfg Config
	var help bool
	flagset := flag.NewFlagSet("whippet", flag.ContinueOnError)
	flagset.StringVar(&cfg.Server, "server", "localhost:1883", "The full URL of the MQTT Server to connect to")
	flagset.StringVar(&cfg.PublishTo, "topic", "", "Topic to publish the messages on")
	flagset.StringVar(&cfg.SubscribeTo, "response-topic", "", "Topic the other party should respond to. Defaults to the publish topic + '/response'")
	flagset.IntVar(&cfg.Qos, "qos", 1, "The QoS to send the messages at")
	flagset.BoolVar(&cfg.Retained, "retained", false, "Are the messages sent with the Retained flag")
	flagset.StringVar(&cfg.ClientID, "clientID", clientID, "A ClientID for the connection")
	flagset.StringVar(&cfg.Username, "username", "", "A Username to authenticate to the MQTT Server")
	flagset.StringVar(&cfg.Password, "password", "", "Password to match Username")
	flagset.DurationVar(&cfg.Timeout, "timeout", defaultTimeout, "Timeout for the operation")
	flagset.BoolVar(&help, "help", false, "Print this help message")
	err = flagset.Parse(args)
	if err != nil {
		return Config{}, false, fmt.Errorf("flagset.Parse: %w", err)
	}
	if help {
		flagset.Usage()
		return Config{}, true, nil

	}
	// check for required flags:
	if cfg.PublishTo == "" {
		return Config{}, false, fmt.Errorf("missing required flag: -topic")
	}
	if cfg.Timeout <= 0 {
		return Config{}, false, fmt.Errorf("timeout must be positive")
	}
	if cfg.SubscribeTo == "" {
		cfg.SubscribeTo = cfg.PublishTo + "/response"
	}
	return cfg, false, nil
}

func makePubPacket(config Config, payload, id []byte) *paho.Publish {
	// construct the publish packet
	return &paho.Publish{
		Topic:   config.PublishTo,
		Payload: payload,
		QoS:     byte(config.Qos),
		Retain:  config.Retained,
		Properties: &paho.PublishProperties{
			CorrelationData:        id,
			ContentType:            "",
			ResponseTopic:          config.SubscribeTo,
			PayloadFormat:          nil,
			MessageExpiry:          nil,
			SubscriptionIdentifier: nil,
			TopicAlias:             nil,
			User:                   nil,
		},
	}
}

func makeRandomID() ([]byte, error) {
	b := make([]byte, correlationDataSize)
	_, err := rand.Read(b)
	if err != nil {
		return nil, fmt.Errorf("rand.Read: %w", err)
	}
	return b, nil
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

func b64(data []byte) string {
	return fmt.Sprintf("%x", data)
}
