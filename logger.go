package logkit

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	timeFormat        = time.RFC3339
	defaultMaxSize    = 100
	defaultMaxBackups = 5
	defaultMaxAgeDays = 30
	nilValue          = "<nil>"
)

// applyFileDefaults fills zero MaxSize, MaxBackups, MaxAge with defaults for lumberjack
func applyFileDefaults(fo FileOptions) FileOptions {
	if fo.MaxSize <= 0 {
		fo.MaxSize = defaultMaxSize
	}
	if fo.MaxBackups <= 0 {
		fo.MaxBackups = defaultMaxBackups
	}
	if fo.MaxAge <= 0 {
		fo.MaxAge = defaultMaxAgeDays
	}
	return fo
}

// loggerState holds closed flag and mutex shared by zerologLogger and its WithFields/WithError children
type loggerState struct {
	mu     sync.RWMutex
	closed bool
}

// zerologLogger implements Logger using zerolog and optional lumberjack for file output
type zerologLogger struct {
	zl        zerolog.Logger
	closer    io.Closer
	closeOnce *sync.Once
	exitOnce  *sync.Once
	state     *loggerState
	exitFunc  func(int)
}

// New builds a Logger from the given options. Defaults are InfoLevel and ConsoleOutput; nil options are ignored
// Use WithLevel, WithOutput, WithFileOptions, WithServiceName, and WithExitFunc to configure
// When Output is FileOutput or BothOutput, FileOptions.Filename must be set; otherwise New returns ErrEmptyFilename
// Unknown Level values are treated as InfoLevel. Call Close on the returned logger when done (e.g. defer) to release file output
func New(opts ...Option) (Logger, error) {
	o := &Options{
		Level:  InfoLevel,
		Output: ConsoleOutput,
	}
	for _, fn := range opts {
		if fn != nil {
			fn(o)
		}
	}
	if o.Output == FileOutput || o.Output == BothOutput {
		if o.FileOptions.Filename == "" {
			return nil, ErrEmptyFilename
		}
	}
	var output io.Writer
	var closer io.Closer
	switch o.Output {
	case ConsoleOutput:
		output = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: timeFormat}
	case FileOutput:
		fo := applyFileDefaults(o.FileOptions)
		lj := &lumberjack.Logger{
			Filename:   fo.Filename,
			MaxSize:    fo.MaxSize,
			MaxBackups: fo.MaxBackups,
			MaxAge:     fo.MaxAge,
			Compress:   fo.Compress,
		}
		output = lj
		closer = lj
	case BothOutput:
		consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: timeFormat}
		fo := applyFileDefaults(o.FileOptions)
		fileWriter := &lumberjack.Logger{
			Filename:   fo.Filename,
			MaxSize:    fo.MaxSize,
			MaxBackups: fo.MaxBackups,
			MaxAge:     fo.MaxAge,
			Compress:   fo.Compress,
		}
		output = zerolog.MultiLevelWriter(consoleWriter, fileWriter)
		closer = fileWriter
	default:
		output = os.Stdout
	}
	zc := zerolog.New(output).With().Timestamp().Caller()
	if o.ServiceName != "" {
		zc = zc.Str("service", o.ServiceName)
	}
	zl := zc.Logger().Level(convertLogLevel(o.Level))
	exitFunc := o.ExitFunc
	if exitFunc == nil {
		exitFunc = os.Exit
	}
	return &zerologLogger{zl: zl, closer: closer, closeOnce: &sync.Once{}, exitOnce: &sync.Once{}, state: &loggerState{}, exitFunc: exitFunc}, nil
}

func (l *zerologLogger) Debug(msg string, fields ...Fields) {
	l.log(l.zl.Debug(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) Info(msg string, fields ...Fields) {
	l.log(l.zl.Info(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) Warn(msg string, fields ...Fields) {
	l.log(l.zl.Warn(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) Error(msg string, fields ...Fields) {
	l.log(l.zl.Error(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) DebugContext(_ context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Debug(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) InfoContext(_ context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Info(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) WarnContext(_ context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Warn(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) ErrorContext(_ context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Error(), msg, fields...) //nolint:zerologlint
}

func (l *zerologLogger) FatalContext(_ context.Context, msg string, fields ...Fields) {
	l.Fatal(msg, fields...)
}

func (l *zerologLogger) Fatal(msg string, fields ...Fields) {
	l.state.mu.Lock()
	l.state.closed = true
	l.logUnchecked(l.zl.WithLevel(zerolog.FatalLevel), msg, fields...)
	l.state.mu.Unlock()
	if err := l.Close(); err != nil {
		_, _ = os.Stderr.WriteString("logger close: " + err.Error() + "\n")
	}
	l.exitOnce.Do(func() { l.exitFunc(1) })
}

func (l *zerologLogger) WithFields(fields Fields) Logger {
	s := sanitizeFields(fields)
	m := make(map[string]any, len(s))
	maps.Copy(m, s)
	return &zerologLogger{zl: l.zl.With().Fields(m).Logger(), closer: l.closer, closeOnce: l.closeOnce, exitOnce: l.exitOnce, state: l.state, exitFunc: l.exitFunc}
}

func (l *zerologLogger) WithError(err error) Logger {
	if isNilInterface(err) {
		return &zerologLogger{zl: l.zl.With().Logger(), closer: l.closer, closeOnce: l.closeOnce, exitOnce: l.exitOnce, state: l.state, exitFunc: l.exitFunc}
	}
	return &zerologLogger{zl: l.zl.With().Str("error", sanitizeMsg(err.Error())).Logger(), closer: l.closer, closeOnce: l.closeOnce, exitOnce: l.exitOnce, state: l.state, exitFunc: l.exitFunc}
}

func (l *zerologLogger) Close() error {
	if l.closer == nil || l.closeOnce == nil {
		return nil
	}
	var err error
	l.closeOnce.Do(func() {
		l.state.mu.Lock()
		l.state.closed = true
		err = l.closer.Close()
		l.state.mu.Unlock()
	})
	return err
}

// sanitizeMsg replaces control characters (including \r, \n) and Unicode line/paragraph separators (U+2028, U+2029) with space to reduce log injection
func sanitizeMsg(msg string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, msg)
}

func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

func sanitizeFields(in Fields) Fields {
	out := make(Fields, len(in))
	for k, v := range in {
		key := sanitizeMsg(k)
		switch val := v.(type) {
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
			out[key] = val
		case string:
			out[key] = sanitizeMsg(val)
		case error:
			if isNilInterface(val) {
				out[key] = nilValue
			} else {
				out[key] = sanitizeMsg(val.Error())
			}
		case fmt.Stringer:
			if isNilInterface(val) {
				out[key] = nilValue
			} else {
				out[key] = sanitizeMsg(val.String())
			}
		case json.Marshaler:
			if isNilInterface(val) {
				out[key] = nilValue
			} else {
				b, err := val.MarshalJSON()
				if err != nil {
					out[key] = sanitizeMsg(fmt.Sprint(val))
				} else {
					out[key] = json.RawMessage([]byte(sanitizeMsg(string(b))))
				}
			}
		case encoding.TextMarshaler:
			if isNilInterface(val) {
				out[key] = nilValue
			} else {
				b, err := val.MarshalText()
				if err != nil {
					out[key] = sanitizeMsg(fmt.Sprint(val))
				} else {
					out[key] = sanitizeMsg(string(b))
				}
			}
		default:
			out[key] = sanitizeMsg(fmt.Sprint(v))
		}
	}
	return out
}

// log checks closed under RLock and skips write if already closed; otherwise delegates to logUnchecked
func (l *zerologLogger) log(event *zerolog.Event, msg string, fields ...Fields) {
	l.state.mu.RLock()
	defer l.state.mu.RUnlock()
	if l.state.closed {
		return
	}
	l.logUnchecked(event, msg, fields...)
}

// logUnchecked writes the event without checking closed; used by log and by Fatal (after which process exits)
// Inline fields are sanitized here; WithFields already sanitizes once when building the zerolog context, so no double sanitization for child-logger fields
func (l *zerologLogger) logUnchecked(event *zerolog.Event, msg string, fields ...Fields) {
	var merged Fields
	if len(fields) == 1 {
		merged = make(Fields, len(fields[0]))
		maps.Copy(merged, fields[0])
	} else if len(fields) > 1 {
		n := 0
		for _, f := range fields {
			n += len(f)
		}
		merged = make(Fields, n)
		for _, f := range fields {
			maps.Copy(merged, f)
		}
	}
	if len(merged) > 0 {
		s := sanitizeFields(merged)
		m := make(map[string]any, len(s))
		maps.Copy(m, s)
		event.Fields(m)
	}
	event.Msg(sanitizeMsg(msg))
}

// convertLogLevel maps Level to zerolog.Level; unknown values map to InfoLevel
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

// convertZerologLevel maps zerolog.Level back to Level; unknown values map to InfoLevel
func convertZerologLevel(level zerolog.Level) Level {
	switch level {
	case zerolog.DebugLevel:
		return DebugLevel
	case zerolog.WarnLevel:
		return WarnLevel
	case zerolog.ErrorLevel:
		return ErrorLevel
	case zerolog.FatalLevel:
		return FatalLevel
	default:
		return InfoLevel
	}
}

// Level returns the minimum level at which this logger emits events
// Implements the optional Leveler interface consumed by SlogHandler
func (l *zerologLogger) Level() Level {
	return convertZerologLevel(l.zl.GetLevel())
}

// noopLogger implements Logger by discarding all output; Fatal does not call os.Exit(1)
type noopLogger struct{}

var _noop Logger = &noopLogger{} //nolint:gochecknoglobals

// Noop returns a Logger that discards all output. Use in tests or when logging is disabled
// Fatal does not call os.Exit(1), so it is safe to use in tests without terminating the process
// FromContext returns Noop when ctx is nil or when no logger was set via IntoContext
func Noop() Logger {
	return _noop
}

func (noopLogger) Debug(_ string, _ ...Fields)                           {}
func (noopLogger) Info(_ string, _ ...Fields)                            {}
func (noopLogger) Warn(_ string, _ ...Fields)                            {}
func (noopLogger) Error(_ string, _ ...Fields)                           {}
func (noopLogger) Fatal(_ string, _ ...Fields)                           {}
func (noopLogger) DebugContext(_ context.Context, _ string, _ ...Fields) {}
func (noopLogger) InfoContext(_ context.Context, _ string, _ ...Fields)  {}
func (noopLogger) WarnContext(_ context.Context, _ string, _ ...Fields)  {}
func (noopLogger) ErrorContext(_ context.Context, _ string, _ ...Fields) {}
func (noopLogger) FatalContext(_ context.Context, _ string, _ ...Fields) {}
func (noopLogger) WithFields(_ Fields) Logger                            { return _noop }
func (noopLogger) WithError(_ error) Logger                              { return _noop }
func (noopLogger) Close() error                                          { return nil }
