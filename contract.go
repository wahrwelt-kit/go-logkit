package logkit

import "context"

// Level is the minimum level at which log events are emitted. Events below this level are dropped
// Pass to WithLevel when building a logger; unknown values are treated as InfoLevel
type Level int

// Log levels; DebugLevel is the lowest, FatalLevel the highest
const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// OutputType selects where log output is written (console, file, or both)
type OutputType int

// ConsoleOutput writes JSON log events to stdout via zerolog's ConsoleWriter (human-readable with colours and timestamps)
const (
	ConsoleOutput OutputType = iota // Write JSON log events to stdout via zerolog's ConsoleWriter (human-readable with colours and timestamps)
	FileOutput                      // Write JSON log events to a rotating file via lumberjack. FileOptions.Filename must be set
	BothOutput                      // Write to both stdout (ConsoleWriter) and a rotating file simultaneously
)

// Fields is a key-value map attached to a log event. Keys and values are sanitized: control characters
// (including \r, \n, NUL, U+2028, U+2029) are replaced with space to reduce log injection
// Multiple Fields passed to one call are merged; later keys override earlier ones
// Shallow-copied when passed to WithFields or log methods; do not mutate the map after passing
type Fields map[string]any

// Leveler is an optional interface that Logger implementations may expose to enable
// level-aware filtering in the slog adapter. If a Logger returned by New implements
// Leveler, SlogHandler uses it so Enabled returns false for levels that would be dropped,
// avoiding unnecessary slog.Record construction
type Leveler interface {
	// Level returns the minimum level at which the logger emits events
	// Used by SlogHandler.Enabled to skip record construction for levels that would be dropped
	Level() Level
}

// Logger is the interface for structured logging. Implementations are safe for concurrent use
// Close releases underlying output (e.g. file); child loggers from WithFields or WithError share the same output-
// closing any of them closes it for all. Call Close only once, typically on the root logger via defer
type Logger interface {
	// Debug emits a debug-level event. No-op when the logger level is above DebugLevel
	Debug(msg string, fields ...Fields)
	// Info emits an info-level event. No-op when the logger level is above InfoLevel
	Info(msg string, fields ...Fields)
	// Warn emits a warning-level event. No-op when the logger level is above WarnLevel
	Warn(msg string, fields ...Fields)
	// Error emits an error-level event. No-op when the logger level is above ErrorLevel
	Error(msg string, fields ...Fields)
	// Fatal emits a fatal-level event, flushes output, then calls os.Exit(1). Does not return
	// Use Noop() or WithExitFunc in tests to prevent process termination
	Fatal(msg string, fields ...Fields)
	// DebugContext is like Debug but accepts a context. The context is currently unused by the
	// zerolog backend; it is part of the interface for future compatibility and middleware use
	DebugContext(ctx context.Context, msg string, fields ...Fields)
	// InfoContext is like Info with a context parameter; see DebugContext for usage notes
	InfoContext(ctx context.Context, msg string, fields ...Fields)
	// WarnContext is like Warn with a context parameter; see DebugContext for usage notes
	WarnContext(ctx context.Context, msg string, fields ...Fields)
	// ErrorContext is like Error with a context parameter; see DebugContext for usage notes
	ErrorContext(ctx context.Context, msg string, fields ...Fields)
	// FatalContext is like Fatal with a context parameter; see DebugContext for usage notes
	FatalContext(ctx context.Context, msg string, fields ...Fields)
	// WithFields returns a child logger with the given fields attached to every subsequent event
	// The child shares the underlying output with the parent; closing either closes both
	WithFields(fields Fields) Logger
	// WithError returns a child logger with err attached as the "error" field on every event
	// If err is nil, no field is added. The child shares the underlying output with the parent
	WithError(err error) Logger
	// Close flushes and releases the underlying output (e.g. the lumberjack file writer)
	// Safe to call multiple times; subsequent calls are no-ops. Child loggers share the same
	// closer - calling Close on any logger in the tree closes output for all of them
	Close() error
}
