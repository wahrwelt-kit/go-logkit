// Package logkit provides a minimal structured logging interface backed by zerolog
// It exposes a Logger interface, optional file rotation via lumberjack, context propagation,
// zerolog hooks, sampling, stack traces, async output via diode, custom writers with optional
// per-write synchronization, context-driven field extractors, and a slog.Handler adapter
//
// # Building a logger
//
// New builds a Logger from options. Defaults are InfoLevel and ConsoleOutput. Nil options are ignored
// Use WithLevel, WithOutput, WithFileOptions, WithServiceName, WithExitFunc, WithHooks, WithSampling,
// WithStackTrace, and WithAsync to configure
// New returns ErrEmptyFilename when Output is FileOutput or BothOutput and FileOptions.Filename is empty
// Unknown Level values are mapped to InfoLevel
//
// # Output
//
// ConsoleOutput writes to stdout in human-readable format (zerolog console writer)
// FileOutput uses lumberjack for rotation: set FileOptions with Filename required; MaxSize, MaxBackups,
// MaxAge use defaults (100 MB, 5 backups, 30 days) when zero. BothOutput writes to stdout and to the file
// Child loggers from WithFields or WithError share the root's output and closer; Close on any of them
// closes the underlying file for all-call Close only on the root logger, typically via defer
//
// # Levels and methods
//
// Logger provides Debug, Info, Warn, Error, and Fatal. Each accepts a message and optional variadic Fields
// multiple Fields are merged (later keys override). DebugContext, InfoContext, and similar methods accept
// context.Context for future use (e.g. trace IDs); the context is not yet used
// Fatal writes the event, calls Close, then calls the configured exit function (default os.Exit(1))
// deferred functions in the caller are not run. For graceful shutdown use Error and an explicit exit path
// Noop returns a logger that discards all output; Fatal on the noop logger does not exit (for tests)
//
// # Context
//
// IntoContext stores a logger in the request context; FromContext retrieves it, or Noop() if absent or ctx is nil
// IntoContext panics if ctx is nil. Use in middleware to set the request-scoped logger and in handlers to retrieve it
//
// # Field helpers
//
// TraceID, RequestID, UserID, Error, Duration, DurationMs, and Component return Fields with conventional keys
// (trace_id, request_id, user_id, error, duration, component). Use them for consistent structured logs
// Duration formats as a string (e.g. "1.5s"); DurationMs formats as int64 milliseconds for ELK/Loki/ClickHouse
// Error returns nil when err is nil (no field added)
//
// # Hooks
//
// WithHooks registers zerolog hooks that run on every event before write. Hooks can mutate the event
// (e.g. add hostname, container_id, OTel trace_id) or perform side effects (e.g. forward Error+ events
// to Sentry, increment a Prometheus counter per level). Hooks run in registration order on the same
// goroutine that emits the event-keep them cheap and non-blocking
//
// # Sampling
//
// WithSampling installs a zerolog sampler that drops events to bound throughput under load
// Common samplers: &zerolog.BasicSampler{N: 10} for 1-of-N, &zerolog.BurstSampler for burst+fallback,
// zerolog.RandomSampler(N) for probabilistic 1/N, and zerolog.LevelSampler for per-level policies
//
// # Stack traces
//
// WithStackTrace enables stack trace extraction for errors attached via WithError
// Requires errors that implement StackTrace() (github.com/pkg/errors, cockroachdb/errors, etc.)
// Standard errors from errors.New or fmt.Errorf without %w do not carry stacks and produce no stack field
// The option installs github.com/rs/zerolog/pkgerrors.MarshalStack as the global zerolog.ErrorStackMarshaler;
// this marshaler is process-global, so concurrent New calls with conflicting StackTrace settings race-
// configure once at startup
//
// # Async output (diode)
//
// WithAsync wraps the configured output in a lock-free diode writer for high-throughput services
// The diode buffers events and drops oldest under sustained pressure instead of blocking the producer-
// trade event loss for predictable producer latency. Recommended only for services emitting >50k events/sec
// AsyncOptions.OnDrop receives the missed-event count when drops occur; surface it as a metric
// Close flushes the diode (with a small wait) before closing the underlying file writer
//
// # Custom writers
//
// WithWriter sets a custom io.Writer that overrides Output/FileOptions; the writer must be safe for concurrent
// Write calls (lumberjack, os.Stdout, *bytes.Buffer guarded by an external mutex). For non-thread-safe writers,
// use WithSyncWriter which wraps the writer in zerolog.SyncWriter (mutex per Write). If the custom writer
// implements io.Closer it is closed on logger Close
//
// # Context extractors
//
// WithContextExtractor registers a ContextExtractor (func(ctx) Fields) that runs on every *Context call to
// pull request-scoped fields - trace_id, request_id, user_id, tenant_id - from context.Context without manual
// WithFields chains in handlers. Multiple extractors compose left-to-right; user-provided Fields override
// extracted Fields. Extractors run on the caller goroutine, so they must be cheap and non-blocking
//
// # Caller skip
//
// WithCallerSkip adjusts the number of stack frames the caller field skips. Use when wrapping logkit in your
// own adapter (e.g. a domain helper that calls logkit.Logger methods) so the caller field points to the
// adapter's caller rather than the adapter itself. Each wrapper function adds one frame to skip
//
// # Concurrency
//
// The hot path is lock-free: a single atomic load checks the closed flag before any work, and an in-flight
// counter ensures Close waits for active log() calls to drain before closing the writer. The original
// RWMutex-based design has been replaced to remove cache-line contention when many goroutines log concurrently.
// Hooks, samplers, and extractors run on the caller goroutine - keep them cheap to preserve this property
//
// # slog integration
//
// SlogHandler returns a slog.Handler that forwards log/slog records to a Logger. Use slog.New(SlogHandler(l))
// to pass a logkit logger to code that expects slog. WithGroup and WithAttrs are supported. The handler is
// the integration point for third-party libraries that emit through log/slog so their events flow through
// the same zerolog backend instead of a separate stdout/JSON stream
//
// # Conventions
//
// Do not log secrets (passwords, tokens, API keys) or unredacted PII in Fields. Control characters (including
// \r, \n, NUL, U+2028, U+2029) in messages and in field keys and values are replaced with space to reduce
// log injection
//
// # Errors
//
// ErrEmptyFilename is the only error returned by New; use errors.Is to detect it
//
// # Thread safety
//
// Logger methods are safe for concurrent use. Close is safe to call from multiple goroutines; only the first
// call has effect and closes the underlying output for all loggers sharing it
//
// # Testing
//
// Use Noop() when a Logger is required but output should be discarded (e.g. in tests or when logging is disabled)
// For asserting log calls in unit tests, use the mock package: mock.NewMockLogger(t) returns a MockLogger that
// implements Logger and records expectations; call EXPECT() to set up and verify calls
package logkit
