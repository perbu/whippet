package whippet

import (
	"fmt"
	"testing"
	"time"
)

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		want     Config
		wantHelp bool
		wantErr  bool
	}{
		{
			name: "Valid Config",
			args: []string{"-server", "mqtt://example.com", "-topic", "testTopic", "-response-topic", "responseTopic", "-qos", "2", "-retained", "-clientID", "client-123", "-username", "user", "-password", "pass"},
			want: Config{
				Server:      "mqtt://example.com",
				PublishTo:   "testTopic",
				SubscribeTo: "responseTopic",
				Qos:         2,
				Retained:    true,
				ClientID:    "client-123", // Assume generateClientID returns "client-123" for this test
				Username:    "user",
				Password:    "pass",
				Timeout:     10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "Just topic",
			args: []string{"-topic", "testTopic"},
			want: Config{
				Server:      "localhost:1883",
				Timeout:     defaultTimeout,
				PublishTo:   "testTopic",
				SubscribeTo: "testTopic",
				Qos:         1,
				Retained:    false,
				ClientID:    "client-123", // Assume generateClientID returns "client-123" for this test
			},
		},
		{
			name:    "Missing topic",
			args:    []string{"-Server", "mqtt://example.com"},
			want:    Config{},
			wantErr: true,
		},
		{
			name:    "Negative timeout",
			args:    []string{"-Server", "mqtt://example.com", "-topic", "testTopic", "-timeout", "-5s"},
			want:    Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, help, err := GetConfig(tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("getConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err := compareConfigs(got, tt.want); err != nil {
				t.Errorf("getConfig() error: %v", err)
			}
			if help != tt.wantHelp {
				t.Errorf("getConfig() help = %v, want %v", help, tt.wantHelp)
			}
		})
	}
}

// compareConfigs compares two Config structs and returns an error if they are different
func compareConfigs(a, b Config) error {
	if a.Server != b.Server {
		return fmt.Errorf("Server fields differ: %s != %s", a.Server, b.Server)
	}
	if a.PublishTo != b.PublishTo {
		return fmt.Errorf("PublishTo fields differ: %s != %s", a.PublishTo, b.PublishTo)
	}
	if a.SubscribeTo != b.SubscribeTo {
		return fmt.Errorf("SubscribeTo fields differ: %s != %s", a.SubscribeTo, b.SubscribeTo)
	}
	if a.Qos != b.Qos {
		return fmt.Errorf("Qos fields differ: %d != %d", a.Qos, b.Qos)
	}
	if a.Retained != b.Retained {
		return fmt.Errorf("Retained fields differ: %t != %t", a.Retained, b.Retained)
	}
	if a.Username != b.Username {
		return fmt.Errorf("Username fields differ: %s != %s", a.Username, b.Username)
	}
	if a.Password != b.Password {
		return fmt.Errorf("Password fields differ: %s != %s", a.Password, b.Password)
	}
	if a.Timeout != b.Timeout {
		return fmt.Errorf("timeout fields differ: %v != %v", a.Timeout, b.Timeout)
	}
	// ignore ClientID
	return nil // No differences
}
