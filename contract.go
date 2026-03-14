package logger

// Level is the minimum level at which log events are emitted.
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// OutputType selects where log output is written.
type OutputType int

const (
	ConsoleOutput OutputType = iota
	FileOutput
	BothOutput
)

// Fields is a key-value map attached to a log event. Multiple Fields passed to
// a single call are merged; later keys override earlier ones for the same key.
type Fields = map[string]any

// Logger is the interface for structured logging. All methods accept an optional
// variadic Fields; multiple maps are merged into one event.
type Logger interface {
	Debug(msg string, fields ...Fields)
	Info(msg string, fields ...Fields)
	Warn(msg string, fields ...Fields)
	Error(msg string, fields ...Fields)
	Fatal(msg string, fields ...Fields)
	WithFields(fields Fields) Logger
	WithError(err error) Logger
}
