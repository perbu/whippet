package whippet

import (
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
	server      string
	mdnsName    string
	publishTo   string
	subscribeTo string
	qos         int
	retained    bool
	clientID    string
	username    string
	password    string
	Timeout     time.Duration
}

func Connect(ctx context.Context, config Config, logger *slog.Logger) (*paho.Client, chan *paho.Publish, error) {
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

func GetConfig(args []string) (Config, bool, error) {
	clientID, err := generateClientID()
	if err != nil {
		return Config{}, false, fmt.Errorf("generateClientID: %w", err)
	}
	var cfg Config
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
	if cfg.publishTo == "" {
		return Config{}, false, fmt.Errorf("missing required flag: -topic")
	}
	if cfg.Timeout <= 0 {
		return Config{}, false, fmt.Errorf("timeout must be positive")
	}
	if cfg.subscribeTo == "" {
		cfg.subscribeTo = cfg.publishTo
	}
	return cfg, false, nil
}

func MakePubPacket(config Config, payload, id []byte) *paho.Publish {
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

func MakeRandomID() ([]byte, error) {
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
