// Package logkit provides a minimal structured logging interface backed by zerolog
// It exposes a Logger interface, optional file rotation via lumberjack, context propagation,
// and a slog.Handler adapter for use with the standard log/slog package
//
// # Building a logger
//
// New builds a Logger from options. Defaults are InfoLevel and ConsoleOutput. Nil options are ignored
// Use WithLevel, WithOutput, WithFileOptions, WithServiceName, and WithExitFunc to configure
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
// # slog integration
//
// SlogHandler returns a slog.Handler that forwards log/slog records to a Logger. Use slog.New(SlogHandler(l))
// to pass a logkit logger to code that expects slog. WithGroup and WithAttrs are supported
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
