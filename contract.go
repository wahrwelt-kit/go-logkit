package logkit

import "context"

// Level is the minimum level at which log events are emitted. Events below this level are dropped.
// Pass to WithLevel when building a logger; unknown values are treated as InfoLevel.
type Level int

// Log levels; DebugLevel is the lowest, FatalLevel the highest.
const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// OutputType selects where log output is written (console, file, or both).
type OutputType int

// Output destinations.
const (
	ConsoleOutput OutputType = iota
	FileOutput
	BothOutput
)

// Fields is a key-value map attached to a log event. Keys and values are sanitized: control characters
// (including \r, \n, NUL, U+2028, U+2029) are replaced with space to reduce log injection.
// Multiple Fields passed to one call are merged; later keys override earlier ones.
// Shallow-copied when passed to WithFields or log methods; do not mutate the map after passing.
type Fields map[string]any

// Logger is the interface for structured logging. Implementations are safe for concurrent use.
// Close releases underlying output (e.g. file); child loggers from WithFields or WithError share the same output—
// closing any of them closes it for all. Call Close only once, typically on the root logger via defer.
type Logger interface {
	Debug(msg string, fields ...Fields)
	Info(msg string, fields ...Fields)
	Warn(msg string, fields ...Fields)
	Error(msg string, fields ...Fields)
	Fatal(msg string, fields ...Fields)
	DebugContext(ctx context.Context, msg string, fields ...Fields)
	InfoContext(ctx context.Context, msg string, fields ...Fields)
	WarnContext(ctx context.Context, msg string, fields ...Fields)
	ErrorContext(ctx context.Context, msg string, fields ...Fields)
	FatalContext(ctx context.Context, msg string, fields ...Fields)
	WithFields(fields Fields) Logger
	WithError(err error) Logger
	Close() error
}
