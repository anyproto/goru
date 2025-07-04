# goru - Multi-Host Goroutine Explorer

A high-performance tool for real-time analysis of Go goroutines across multiple hosts, with both TUI and Web interfaces.

## Features

- **Dual ingestion paths**: Live HTTP polling of `/debug/pprof/goroutine` endpoints and local file reading (plain text or gzip)
- **Multi-host support**: Monitor goroutines across multiple Go applications simultaneously
- **Smart grouping**: Groups goroutines by stack trace and state
- **Real-time updates**: Live monitoring with configurable intervals
- **File follow mode**: Tail-like functionality for growing dump files
- **Minimal dependencies**: CGO-free, single binary distribution

## Installation

```bash
go install github.com/anyproto/goru/cmd/goru@latest
```

Or build from source:

```bash
git clone https://github.com/anyproto/goru
cd goru
make build
```

## Usage

### Monitor live endpoints

```bash
goru --targets=localhost:6060,localhost:6061 --interval=2s
```

### Analyze dump files

```bash
goru --files="dumps/*.txt,dumps/*.gz"
```

### Follow growing files

```bash
goru --files="current.dump" --follow --interval=1s
```

### Run with test data

```bash
# Create a test dump file
goru --files="test.dump"

# Monitor multiple files
goru --files="dumps/*.txt,dumps/*.gz" --mode=tui
```

### Configuration

goru supports configuration via:
1. Command-line flags (highest priority)
2. Environment variables (prefix: `goru_`)
3. YAML config file

## Development Status

### Completed
- ✅ Repository structure and CI/CD setup
- ✅ Configuration management (flags, env, YAML)
- ✅ Core data models
- ✅ Goroutine dump parser with address stripping
- ✅ HTTP collector with worker pool
- ✅ File collector with glob and gzip support
- ✅ Snapshot diff algorithm (O(n) comparison)
- ✅ In-memory store with atomic updates
- ✅ Orchestrator for coordinating collectors
- ✅ Telemetry with structured logging and optional pprof
- ✅ TUI implementation with Bubble Tea

### In Progress
- 🚧 Web UI with embedded SPA and WebSocket support

## Architecture

```
┌────────────────────────────────────────────────────────────────────────────┐
│                                   goru                                     │
│                                                                            │
│    Sources (concrete impls of collector.Source)                            │
│   ┌──────────────┐   ┌──────────────┐                                      │
│   │  HTTPSource  │   │  FileSource  │                                      │
│   │ polls hosts  │   │ reads dumps  │                                      │
│   └─────▲────────┘   └─────▲────────┘                                      │
│         │ snapshots        │ snapshots                                     │
│         ├──────────────┬───────┘                                           │
│         ▼              ▼                                                   │
│   ┌─────────────────────────┐                                              │
│   │        Parser           │  (strip addrs, extract wait-durations)       │
│   └────────────▲────────────┘                                              │
│                │ batches                                                   │
│         ┌──────┴──────┐                                                    │
│         │ Orchestrator│  (fans-in snapshots, computes diffs)               │
│         └──────▲──────┘                                                    │
│                │                                                           │
│        ┌───────┴────────┐   read-only                      ┌────────────┐  │
│        │  Snapshot Store│◄────────────────────────────────►│  Web UI    │  │
│        └────────────────┘  broadcast on change (WebSocket) └────────────┘  │
│                │                                                           │
│                ▼                                                           │
│           ┌────────┐                                                       │
│           │  TUI   │                                                       │
│           └────────┘                                                       │
│                                                                            │
└────────────────────────────────────────────────────────────────────────────┘
```

## License

MIT