package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anyproto/goru/internal/collector"
	"github.com/anyproto/goru/internal/collector/file"
	"github.com/anyproto/goru/internal/collector/http"
	"github.com/anyproto/goru/internal/config"
	"github.com/anyproto/goru/internal/orchestrator"
	"github.com/anyproto/goru/internal/store"
	"github.com/anyproto/goru/internal/telemetry"
	"github.com/anyproto/goru/internal/tui"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Check for version flag
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("goru %s (built %s)\n", version, buildTime)
		return nil
	}

	// Load configuration
	cfg := config.New()
	if err := cfg.Load(); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Initialize logger
	logger := telemetry.NewLogger(cfg.Log.Level, cfg.Log.JSON)
	logger.Info("Starting goru",
		telemetry.String("version", version),
		telemetry.String("mode", string(cfg.Mode)),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Received shutdown signal")
		cancel()
	}()

	// Start pprof if configured
	if err := telemetry.StartPProf(ctx, cfg.PProf, logger); err != nil {
		return fmt.Errorf("starting pprof: %w", err)
	}

	// Create store
	s := store.New()

	// Create collectors
	var sources []collector.Source

	// HTTP sources
	if len(cfg.Targets) > 0 {
		// Register all HTTP targets with the store so they appear in UI even if unreachable
		s.RegisterHosts(cfg.Targets)
		
		httpSource := http.New(cfg.Targets, cfg.Interval, cfg.Timeout, 5) // 5 workers
		sources = append(sources, httpSource)
		logger.Info("Added HTTP source",
			telemetry.Int("targets", len(cfg.Targets)),
			telemetry.Duration("interval", cfg.Interval),
			telemetry.Duration("timeout", cfg.Timeout),
		)
	}

	// File sources
	if len(cfg.Files) > 0 {
		fileSource := file.New(cfg.Files, cfg.Follow, cfg.Interval)
		sources = append(sources, fileSource)
		logger.Info("Added file source",
			telemetry.Int("patterns", len(cfg.Files)),
			telemetry.String("follow", fmt.Sprintf("%v", cfg.Follow)),
		)
	}

	if len(sources) == 0 {
		return fmt.Errorf("no sources configured (use --targets or --files)")
	}

	// Create and start orchestrator
	orch := orchestrator.New(s, sources...)

	// Start orchestrator in background
	orchErrCh := make(chan error, 1)
	go func() {
		if err := orch.Start(ctx); err != nil {
			orchErrCh <- fmt.Errorf("orchestrator error: %w", err)
		}
	}()

	// Start UI based on mode
	var uiErr error

	switch cfg.Mode {
	case config.ModeTUI, config.ModeBoth:
		// Create TUI model
		model := tui.New(s)

		// Create tea program
		p := tea.NewProgram(model, tea.WithAltScreen())

		// Run TUI
		logger.Info("Starting TUI")
		if _, err := p.Run(); err != nil {
			uiErr = fmt.Errorf("TUI error: %w", err)
		}

	case config.ModeWeb:
		// TODO: Implement web server
		logger.Info("Web mode not yet implemented")
		<-ctx.Done()

	default:
		return fmt.Errorf("invalid mode: %s", cfg.Mode)
	}

	// Check for orchestrator errors
	select {
	case err := <-orchErrCh:
		return err
	default:
	}

	if uiErr != nil {
		return uiErr
	}

	logger.Info("Shutdown complete")
	return nil
}
