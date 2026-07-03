# go-logkit

[![CI](https://github.com/wahrwelt-kit/go-logkit/actions/workflows/ci.yml/badge.svg)](https://github.com/wahrwelt-kit/go-logkit/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/wahrwelt-kit/go-logkit.svg)](https://pkg.go.dev/github.com/wahrwelt-kit/go-logkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/wahrwelt-kit/go-logkit)](https://goreportcard.com/report/github.com/wahrwelt-kit/go-logkit)

Small structured logging kit backed by `zerolog`.

## Install

```bash
go get github.com/wahrwelt-kit/go-logkit
```

```go
import "github.com/wahrwelt-kit/go-logkit"
```

## Features

- `Logger` interface: `Debug`, `Info`, `Warn`, `Error`, `Fatal`, context variants, `WithFields`, `WithError`, `Close`
- JSON stdout by default, pretty console for local development, rotating file output via `lumberjack`
- context storage with `IntoContext` / `FromContext`
- context extractors for request-scoped fields
- field helpers for trace ID, request ID, user ID, errors, duration, and component
- default and configurable redaction for sensitive keys, including nested JSON fields
- hooks, sampling, stack traces, async diode writer, custom writers
- `log/slog` handler bridge
- `Noop()` logger and generated mock for tests

## Basic Usage

```go
l, err := logkit.New(
    logkit.WithLevel(logkit.InfoLevel),
    logkit.WithServiceName("api"),
)
if err != nil {
    log.Fatal(err)
}
defer l.Close()

l.Info("started",
    logkit.Component("http"),
    logkit.DurationMs(153*time.Millisecond),
    logkit.Fields{"authorization": "Bearer secret"},
)
```

Output is JSON. Sensitive values are redacted before writing:

```json
{"level":"info","service":"api","component":"http","duration_ms":153,"authorization":"[REDACTED]","message":"started"}
```

## Redaction

Common sensitive field keys are redacted by default: authorization, proxy authorization, cookies, passwords, credential token keys, secrets, API keys, session IDs, CSRF/XSRF values, and private keys. Matching is case-insensitive and ignores separators like `_`, `-`, `.`, and spaces.

```go
l, err := logkit.New(
    logkit.WithSensitiveKeys("tenant_license"),
    logkit.WithRedactor(func(key string, value any) (any, bool) {
        if key == "email" {
            return "redacted@example.invalid", true
        }
        return nil, false
    }),
)
```

`json.Marshaler` values are decoded before writing, so nested object keys are redacted too:

```go
l.Info("token response", logkit.Fields{"payload": tokenPayload})
```

## HTTP Context Example

```go
l, err := logkit.New(
    logkit.WithServiceName("api"),
    logkit.WithContextExtractor(func(ctx context.Context) logkit.Fields {
        fields := logkit.Fields{}
        if requestID, ok := ctx.Value(requestIDKey{}).(string); ok {
            fields["request_id"] = requestID
        }
        if traceID, ok := ctx.Value(traceIDKey{}).(string); ok {
            fields["trace_id"] = traceID
        }
        return fields
    }),
)
if err != nil {
    log.Fatal(err)
}

func middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := logkit.IntoContext(r.Context(), l)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func handler(w http.ResponseWriter, r *http.Request) {
    logkit.FromContext(r.Context()).InfoContext(r.Context(), "request handled",
        logkit.Fields{"method": r.Method, "path": r.URL.Path},
    )
}
```

## Output Modes

```go
logkit.WithOutput(logkit.ConsoleOutput)       // JSON stdout, production default
logkit.WithOutput(logkit.PrettyConsoleOutput) // human-readable stdout for local development
logkit.WithOutput(logkit.FileOutput)          // rotating JSON file
logkit.WithOutput(logkit.BothOutput)          // JSON stdout + rotating JSON file
```

File output requires a filename:

```go
l, err := logkit.New(
    logkit.WithOutput(logkit.FileOutput),
    logkit.WithFileOptions(logkit.FileOptions{
        Filename:   "/var/log/app.log",
        MaxSize:    100,
        MaxBackups: 5,
        MaxAge:     30,
        Compress:   true,
    }),
)
```

## Production Options

```go
l, err := logkit.New(
    logkit.WithServiceName("api"),
    logkit.WithStackTrace(),
    logkit.WithSampling(&zerolog.LevelSampler{
        DebugSampler: &zerolog.BasicSampler{N: 10},
    }),
    logkit.WithAsync(logkit.AsyncOptions{
        Size:         4096,
        PollInterval: 10 * time.Millisecond,
        OnDrop: func(missed int) {
            droppedLogs.Add(float64(missed))
        },
    }),
)
```

`WithAsync` is for very high log volume paths. It trades possible event loss for predictable producer latency. `AsyncOptions.Size` must be positive.

## Hooks and Custom Writers

```go
l, err := logkit.New(
    logkit.WithHooks(hostnameHook{}),
    logkit.WithSyncWriter(writer),
)
```

Use `WithWriter` for writers that are already safe for concurrent writes. Use `WithSyncWriter` for buffers, network writers, or direct files that need per-write serialization.

## slog Bridge

```go
l, err := logkit.New(logkit.WithServiceName("api"))
if err != nil {
    log.Fatal(err)
}

slogLogger := slog.New(logkit.SlogHandler(l))
slogLogger.InfoContext(ctx, "handled", "status", 200)
```

`SlogHandler` preserves groups, keeps the original slog caller for logkit loggers, and forwards records through context-aware log methods, so configured context extractors still run.

## API

| Symbol | Description |
|--------|-------------|
| `Logger` | Structured logger interface with level methods, context methods, children, and `Close` |
| `Level` | `DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, `FatalLevel` |
| `OutputType` | `ConsoleOutput`, `PrettyConsoleOutput`, `FileOutput`, `BothOutput` |
| `Fields` | `map[string]any` for structured event fields |
| `New(opts...)` | Builds a logger |
| `Noop()` | Logger that discards all output |
| `IntoContext`, `FromContext` | Store and retrieve logger from context |
| `WithLevel`, `WithOutput`, `WithFileOptions`, `WithServiceName`, `WithExitFunc` | Core options |
| `WithHooks`, `WithSampling`, `WithStackTrace`, `WithAsync` | Production behavior options |
| `WithContextExtractor`, `WithCallerSkip`, `WithWriter`, `WithSyncWriter` | Integration options |
| `WithSensitiveKeys`, `WithRedactor` | Redaction options |
| `TraceID`, `RequestID`, `UserID`, `Error`, `Duration`, `DurationMs`, `Component` | Field helpers |
| `SlogHandler` | `log/slog` handler bridge |

## Testing

```bash
make test
make test-race
make test-fuzz FUZZTIME=1s
make lint
```

Use `Noop()` when output is not needed. For expectation-based tests, use `mock.NewMockLogger(t)`.
