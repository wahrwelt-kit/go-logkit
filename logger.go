package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

const timeFormat = time.RFC3339

type zerologLogger struct {
	zl zerolog.Logger
}

// New builds a Logger from the given options. Defaults are InfoLevel and ConsoleOutput.
// Returns ErrEmptyFilename if Output is FileOutput or BothOutput and Filename is empty.
func New(opts ...Option) (Logger, error) {
	o := &Options{
		Level:  InfoLevel,
		Output: ConsoleOutput,
	}
	for _, fn := range opts {
		fn(o)
	}
	if o.Output == FileOutput || o.Output == BothOutput {
		if o.FileOptions.Filename == "" {
			return nil, ErrEmptyFilename
		}
	}
	var output io.Writer
	switch o.Output {
	case ConsoleOutput:
		output = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: timeFormat}
	case FileOutput:
		output = &lumberjack.Logger{
			Filename:   o.FileOptions.Filename,
			MaxSize:    o.FileOptions.MaxSize,
			MaxBackups: o.FileOptions.MaxBackups,
			MaxAge:     o.FileOptions.MaxAge,
			Compress:   o.FileOptions.Compress,
		}
	case BothOutput:
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: timeFormat}
		fileWriter := &lumberjack.Logger{
			Filename:   o.FileOptions.Filename,
			MaxSize:    o.FileOptions.MaxSize,
			MaxBackups: o.FileOptions.MaxBackups,
			MaxAge:     o.FileOptions.MaxAge,
			Compress:   o.FileOptions.Compress,
		}
		output = zerolog.MultiLevelWriter(consoleWriter, fileWriter)
	default:
		output = os.Stdout
	}
	zl := zerolog.New(output).With().Timestamp().Caller().Logger()
	zl = zl.Level(convertLogLevel(o.Level))
	return &zerologLogger{zl: zl}, nil
}

func (l *zerologLogger) Debug(msg string, fields ...Fields) {
	l.log(l.zl.Debug(), msg, fields...)
}

func (l *zerologLogger) Info(msg string, fields ...Fields) {
	l.log(l.zl.Info(), msg, fields...)
}

func (l *zerologLogger) Warn(msg string, fields ...Fields) {
	l.log(l.zl.Warn(), msg, fields...)
}

func (l *zerologLogger) Error(msg string, fields ...Fields) {
	l.log(l.zl.Error(), msg, fields...)
}

func (l *zerologLogger) Fatal(msg string, fields ...Fields) {
	l.log(l.zl.Fatal(), msg, fields...)
}

func (l *zerologLogger) WithFields(fields Fields) Logger {
	return &zerologLogger{zl: l.zl.With().Fields(fields).Logger()}
}

func (l *zerologLogger) WithError(err error) Logger {
	return &zerologLogger{zl: l.zl.With().Err(err).Logger()}
}

func (l *zerologLogger) log(event *zerolog.Event, msg string, fields ...Fields) {
	if len(fields) > 0 {
		merged := make(Fields)
		for _, f := range fields {
			for k, v := range f {
				merged[k] = v
			}
		}
		event.Fields(merged)
	}
	event.Msg(msg)
}

func convertLogLevel(level Level) zerolog.Level {
	switch level {
	case DebugLevel:
		return zerolog.DebugLevel
	case InfoLevel:
		return zerolog.InfoLevel
	case WarnLevel:
		return zerolog.WarnLevel
	case ErrorLevel:
		return zerolog.ErrorLevel
	case FatalLevel:
		return zerolog.FatalLevel
	default:
		return zerolog.InfoLevel
	}
}

type noopLogger struct{}

// Noop returns a Logger that discards all output. Use in tests or when logging is disabled.
func Noop() Logger {
	return &noopLogger{}
}

func (noopLogger) Debug(_ string, _ ...Fields) {}
func (noopLogger) Info(_ string, _ ...Fields)  {}
func (noopLogger) Warn(_ string, _ ...Fields)  {}
func (noopLogger) Error(_ string, _ ...Fields) {}
func (noopLogger) Fatal(_ string, _ ...Fields) {}
func (noopLogger) WithFields(_ Fields) Logger  { return &noopLogger{} }
func (noopLogger) WithError(_ error) Logger    { return &noopLogger{} }
