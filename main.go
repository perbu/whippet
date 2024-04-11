package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"github.com/eclipse/paho.golang/paho"
	"github.com/perbu/whippet/whippet"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

//go:embed .version
var embeddedVersion string

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	err := run(ctx, os.Stderr, os.Stdout, os.Stdin, os.Args[1:], os.Environ())
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logoutput, output io.Writer, input io.Reader, args, env []string) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	logHandle := slog.NewTextHandler(logoutput, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(logHandle)
	config, showhelp, err := whippet.GetConfig(args)
	if err != nil {
		return fmt.Errorf("getConfig: %w", err)
	}
	if showhelp {
		return nil
	}
	logger.Info("whippet starting up", "version", embeddedVersion)
	client, msgChan, err := whippet.Connect(runCtx, config, logger)
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
	randomID, err := whippet.MakeRandomID()
	if err != nil {
		return fmt.Errorf("randomBytes: %w", err)
	}

	// wait for the response
	ctxTimeout, timeoutCancel := context.WithTimeout(runCtx, config.Timeout)
	defer timeoutCancel()
	pubPacket := whippet.MakePubPacket(config, payload, randomID)
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
