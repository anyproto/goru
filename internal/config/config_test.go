package config

import (
	"os"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *Config
		wantErr bool
	}{
		{
			name: "valid with targets",
			setup: func() *Config {
				c := New()
				c.Targets = []string{"localhost:8080"}
				return c
			},
			wantErr: false,
		},
		{
			name: "valid with files",
			setup: func() *Config {
				c := New()
				c.Files = []string{"dump.txt"}
				c.Mode = ModeWeb
				c.Log.Level = "debug"
				return c
			},
			wantErr: false,
		},
		{
			name: "no sources",
			setup: func() *Config {
				c := New()
				c.Targets = nil
				c.Files = nil
				return c
			},
			wantErr: true,
		},
		{
			name: "invalid mode",
			setup: func() *Config {
				c := New()
				c.Targets = []string{"localhost:8080"}
				c.Mode = "invalid"
				return c
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			setup: func() *Config {
				c := New()
				c.Targets = []string{"localhost:8080"}
				c.Log.Level = "invalid"
				return c
			},
			wantErr: true,
		},
		{
			name: "interval too small",
			setup: func() *Config {
				c := New()
				c.Targets = []string{"localhost:8080"}
				c.Interval = 50 * time.Millisecond
				return c
			},
			wantErr: true,
		},
		{
			name: "TLS cert without key",
			setup: func() *Config {
				c := New()
				c.Targets = []string{"localhost:8080"}
				c.Mode = ModeWeb
				c.Web.TLSCert = "cert.pem"
				return c
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.setup()
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigModes(t *testing.T) {
	tests := []struct {
		mode   Mode
		hasWeb bool
		hasTUI bool
	}{
		{ModeTUI, false, true},
		{ModeWeb, true, false},
		{ModeBoth, true, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			c := &Config{Mode: tt.mode}
			if got := c.HasWeb(); got != tt.hasWeb {
				t.Errorf("HasWeb() = %v, want %v", got, tt.hasWeb)
			}
			if got := c.HasTUI(); got != tt.hasTUI {
				t.Errorf("HasTUI() = %v, want %v", got, tt.hasTUI)
			}
		})
	}
}

func TestConfigPrecedence(t *testing.T) {
	// Reset flags for this test
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)

	// Set environment variable
	os.Setenv("goru_LOG_LEVEL", "debug")
	defer os.Unsetenv("goru_LOG_LEVEL")

	// Create config with defaults
	c := New()

	// Simulate flag parsing
	os.Args = []string{"test", "--log.level=error", "--targets=localhost:8080"}

	err := c.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Flag should override env var
	if c.Log.Level != "error" {
		t.Errorf("Log.Level = %v, want error (flag should override env)", c.Log.Level)
	}
}
