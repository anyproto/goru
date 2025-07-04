package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

type Mode string

const (
	ModeTUI  Mode = "tui"
	ModeWeb  Mode = "web"
	ModeBoth Mode = "both"
)

type Config struct {
	Targets  []string      `yaml:"targets" envconfig:"goru_TARGETS"`
	Files    []string      `yaml:"files" envconfig:"goru_FILES"`
	Follow   bool          `yaml:"follow" envconfig:"goru_FOLLOW"`
	Interval time.Duration `yaml:"interval" envconfig:"goru_INTERVAL"`
	Timeout  time.Duration `yaml:"timeout" envconfig:"goru_TIMEOUT"`
	Mode     Mode          `yaml:"mode" envconfig:"goru_MODE"`
	PProf    string        `yaml:"pprof" envconfig:"goru_PPROF"`

	Web struct {
		Host    string `yaml:"host" envconfig:"goru_WEB_HOST"`
		Port    int    `yaml:"port" envconfig:"goru_WEB_PORT"`
		NoOpen  bool   `yaml:"no_open" envconfig:"goru_WEB_NO_OPEN"`
		TLSCert string `yaml:"tls_cert" envconfig:"goru_WEB_TLS_CERT"`
		TLSKey  string `yaml:"tls_key" envconfig:"goru_WEB_TLS_KEY"`
	} `yaml:"web"`

	Log struct {
		Level string `yaml:"level" envconfig:"goru_LOG_LEVEL"`
		JSON  bool   `yaml:"json" envconfig:"goru_LOG_JSON"`
	} `yaml:"log"`

	ConfigFile string `yaml:"-"`
}

func New() *Config {
	return &Config{
		Interval: 2 * time.Second,
		Timeout:  30 * time.Second,
		Mode:     ModeTUI,
		Web: struct {
			Host    string `yaml:"host" envconfig:"goru_WEB_HOST"`
			Port    int    `yaml:"port" envconfig:"goru_WEB_PORT"`
			NoOpen  bool   `yaml:"no_open" envconfig:"goru_WEB_NO_OPEN"`
			TLSCert string `yaml:"tls_cert" envconfig:"goru_WEB_TLS_CERT"`
			TLSKey  string `yaml:"tls_key" envconfig:"goru_WEB_TLS_KEY"`
		}{
			Host: "localhost",
			Port: 8080,
		},
		Log: struct {
			Level string `yaml:"level" envconfig:"goru_LOG_LEVEL"`
			JSON  bool   `yaml:"json" envconfig:"goru_LOG_JSON"`
		}{
			Level: "info",
		},
	}
}

func (c *Config) Load() error {
	// 1. Define flags
	pflag.StringSliceVar(&c.Targets, "targets", c.Targets, "Comma-separated host:port list to poll via HTTP")
	pflag.StringSliceVar(&c.Files, "files", c.Files, "Paths or globs of goroutine-dump files (.txt or .gz)")
	pflag.BoolVar(&c.Follow, "follow", c.Follow, "Re-read growing files (tail-like)")
	pflag.DurationVar(&c.Interval, "interval", c.Interval, "Poll interval for HTTP targets or rescan interval for files")
	pflag.DurationVar(&c.Timeout, "timeout", c.Timeout, "HTTP timeout for fetching goroutine dumps")
	pflag.StringVar((*string)(&c.Mode), "mode", string(c.Mode), "Run mode: tui, web, or both")
	pflag.StringVar(&c.PProf, "pprof", c.PProf, "Host:port to expose pprof endpoints for self-inspection")

	pflag.StringVar(&c.Web.Host, "web.host", c.Web.Host, "Web server host")
	pflag.IntVar(&c.Web.Port, "web.port", c.Web.Port, "Web server port")
	pflag.BoolVar(&c.Web.NoOpen, "web.no-open", c.Web.NoOpen, "Don't open browser automatically")
	pflag.StringVar(&c.Web.TLSCert, "web.tls-cert", c.Web.TLSCert, "TLS certificate file")
	pflag.StringVar(&c.Web.TLSKey, "web.tls-key", c.Web.TLSKey, "TLS key file")

	pflag.StringVar(&c.Log.Level, "log.level", c.Log.Level, "Log level (debug, info, warn, error)")
	pflag.BoolVar(&c.Log.JSON, "log.json", c.Log.JSON, "Use JSON format for logs")

	pflag.StringVar(&c.ConfigFile, "config", c.ConfigFile, "Config file path")

	pflag.Parse()

	// 2. Load from config file if specified
	if c.ConfigFile != "" {
		if err := c.loadFromFile(c.ConfigFile); err != nil {
			return fmt.Errorf("loading config file: %w", err)
		}
	}

	// 3. Load from environment variables
	if err := envconfig.Process("goru", c); err != nil {
		return fmt.Errorf("processing env vars: %w", err)
	}

	// 4. Re-parse flags to override config file and env vars
	pflag.Parse()

	// 5. Validate
	return c.Validate()
}

func (c *Config) loadFromFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	return decoder.Decode(c)
}

func (c *Config) Validate() error {
	// At least one source must be specified
	if len(c.Targets) == 0 && len(c.Files) == 0 {
		return fmt.Errorf("at least one of --targets or --files must be specified")
	}

	// Validate mode
	switch c.Mode {
	case ModeTUI, ModeWeb, ModeBoth:
		// valid
	default:
		return fmt.Errorf("invalid mode: %s (must be tui, web, or both)", c.Mode)
	}

	// Validate log level
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
		c.Log.Level = strings.ToLower(c.Log.Level)
	default:
		return fmt.Errorf("invalid log level: %s", c.Log.Level)
	}

	// Validate TLS config
	if (c.Web.TLSCert != "" && c.Web.TLSKey == "") || (c.Web.TLSCert == "" && c.Web.TLSKey != "") {
		return fmt.Errorf("both --web.tls-cert and --web.tls-key must be specified for TLS")
	}

	// Validate interval
	if c.Interval < 100*time.Millisecond {
		return fmt.Errorf("interval must be at least 100ms")
	}

	return nil
}

func (c *Config) HasWeb() bool {
	return c.Mode == ModeWeb || c.Mode == ModeBoth
}

func (c *Config) HasTUI() bool {
	return c.Mode == ModeTUI || c.Mode == ModeBoth
}
