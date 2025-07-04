package telemetry

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
)

// Logger provides structured logging
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Field represents a log field
type Field struct {
	Key   string
	Value interface{}
}

// String creates a string field
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Int creates an int field
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Error creates an error field
func Error(err error) Field {
	return Field{Key: "error", Value: err}
}

// Duration creates a duration field
func Duration(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// simpleLogger is a basic implementation using standard library
type simpleLogger struct {
	logger *log.Logger
	level  LogLevel
	json   bool
	fields []Field
}

type LogLevel int

const (
	DebugLevel LogLevel = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// NewLogger creates a new logger
func NewLogger(level string, json bool) Logger {
	var logLevel LogLevel
	switch level {
	case "debug":
		logLevel = DebugLevel
	case "warn":
		logLevel = WarnLevel
	case "error":
		logLevel = ErrorLevel
	default:
		logLevel = InfoLevel
	}

	flags := 0
	if !json {
		flags = log.LstdFlags | log.Lmicroseconds
	}

	return &simpleLogger{
		logger: log.New(os.Stderr, "", flags),
		level:  logLevel,
		json:   json,
	}
}

func (l *simpleLogger) shouldLog(level LogLevel) bool {
	return level >= l.level
}

func (l *simpleLogger) log(level LogLevel, levelStr, msg string, fields []Field) {
	if !l.shouldLog(level) {
		return
	}

	allFields := append(l.fields, fields...)

	if l.json {
		// Simple JSON format
		fmt.Fprintf(os.Stderr, `{"level":"%s","msg":"%s"`, levelStr, msg)
		for _, f := range allFields {
			switch v := f.Value.(type) {
			case string:
				fmt.Fprintf(os.Stderr, `,"%s":"%s"`, f.Key, v)
			case error:
				fmt.Fprintf(os.Stderr, `,"%s":"%s"`, f.Key, v.Error())
			default:
				fmt.Fprintf(os.Stderr, `,"%s":%v`, f.Key, v)
			}
		}
		fmt.Fprintln(os.Stderr, "}")
	} else {
		// Human-readable format
		parts := []interface{}{levelStr, msg}
		for _, f := range allFields {
			parts = append(parts, fmt.Sprintf("%s=%v", f.Key, f.Value))
		}
		l.logger.Println(parts...)
	}
}

func (l *simpleLogger) Debug(msg string, fields ...Field) {
	l.log(DebugLevel, "DEBUG", msg, fields)
}

func (l *simpleLogger) Info(msg string, fields ...Field) {
	l.log(InfoLevel, "INFO", msg, fields)
}

func (l *simpleLogger) Warn(msg string, fields ...Field) {
	l.log(WarnLevel, "WARN", msg, fields)
}

func (l *simpleLogger) Error(msg string, fields ...Field) {
	l.log(ErrorLevel, "ERROR", msg, fields)
}

func (l *simpleLogger) With(fields ...Field) Logger {
	return &simpleLogger{
		logger: l.logger,
		level:  l.level,
		json:   l.json,
		fields: append(l.fields, fields...),
	}
}

// StartPProf starts the pprof HTTP server if configured
func StartPProf(ctx context.Context, addr string, logger Logger) error {
	if addr == "" {
		return nil
	}

	mux := http.NewServeMux()

	// Register pprof handlers
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Register specific pprof profiles
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logger.Info("Starting pprof server", String("addr", addr))

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("pprof server error", Error(err))
		}
	}()

	return nil
}
