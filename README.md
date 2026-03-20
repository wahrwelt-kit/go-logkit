# go-logkit

[![CI](https://github.com/takuya-go-kit/go-logkit/actions/workflows/ci.yml/badge.svg)](https://github.com/takuya-go-kit/go-logkit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/takuya-go-kit/go-logkit.svg)](https://pkg.go.dev/github.com/takuya-go-kit/go-logkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/takuya-go-kit/go-logkit)](https://goreportcard.com/report/github.com/takuya-go-kit/go-logkit)

Structured logging interface backed by zerolog.

## Install

```bash
go get github.com/takuya-go-kit/go-logkit
```

```go
import "github.com/takuya-go-kit/go-logkit"
```

## Features

- **Logger** interface: Debug, Info, Warn, Error, Fatal; WithFields, WithError
- **Output**: console, file, or both (lumberjack rotation for file)
- **Context**: IntoContext / FromContext to pass logger in request context
- **Fields**: TraceID, RequestID, UserID, Error, Duration, Component for consistent keys
- **Options**: WithLevel, WithOutput, WithFileOptions, WithServiceName
- **Noop()**: silent logger for tests

## Example

```go
l, err := logkit.New(
    logkit.WithLevel(logkit.InfoLevel),
    logkit.WithOutput(logkit.ConsoleOutput),
    logkit.WithServiceName("api"),
)
if err != nil {
    log.Fatal(err)
}
l.Info("started", logkit.RequestID("req-1"), logkit.Duration(time.Second))

ctx := logkit.IntoContext(r.Context(), l)
lFromCtx := logkit.FromContext(ctx)
```

## API

| Symbol | Description |
|--------|-------------|
| Logger | Interface: Debug, Info, Warn, Error, Fatal(msg, fields...); WithFields, WithError |
| Level | DebugLevel, InfoLevel, WarnLevel, ErrorLevel, FatalLevel |
| OutputType | ConsoleOutput, FileOutput, BothOutput |
| Fields | map[string]any for log event key-value data |
| New(opts...) | Build Logger; returns ErrEmptyFilename if file output without Filename |
| Noop() | Logger that discards all output |
| IntoContext, FromContext | Store/retrieve Logger in context |
| WithLevel, WithOutput, WithFileOptions, WithServiceName, WithExitFunc | Options (WithExitFunc overrides exit on Fatal) |
| TraceID, RequestID, UserID, Error, Duration, DurationMs, Component | Field helpers |
| SlogHandler | Returns slog.Handler for use with log/slog |
| DebugContext, InfoContext, WarnContext, ErrorContext, FatalContext | Context-aware log methods (ctx reserved for future tracing) |

## Resource cleanup

When using `FileOutput` or `BothOutput`, call `Close()` to flush and release the log file:

```go
l, err := logkit.New(
    logkit.WithOutput(logkit.FileOutput),
    logkit.WithFileOptions(logkit.FileOptions{Filename: "/var/log/app.log"}),
)
if err != nil {
    log.Fatal(err)
}
defer l.Close()
```

`Close()` is safe to call multiple times. Only the first call has effect.
