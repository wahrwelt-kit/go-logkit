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
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"github.com/rs/zerolog/pkgerrors"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	timeFormat        = time.RFC3339
	defaultMaxSize    = 100
	defaultMaxBackups = 5
	defaultMaxAgeDays = 30
	nilValue          = "<nil>"
	// callerWrapperFrames accounts for logkit's wrappers between the user call site and zerolog's event.Msg
	// Stack at Msg time: event.Msg <- logUnchecked <- log <- (Info|Debug|...) <- user code
	// Default zerolog CallerSkipFrameCount lands on the direct caller of Msg (logUnchecked); add 3 to reach user code
	// *Context methods inline extractor handling and call log directly, so they share the same depth
	callerWrapperFrames = 3
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

// loggerState holds the closed flag and inflight write tracker shared by zerologLogger and its
// WithFields/WithError children. closed is an atomic.Bool so the hot path log() avoids RWMutex contention;
// inflight is a sync.WaitGroup that Close uses to wait for in-flight writes to drain before closing the writer
type loggerState struct {
	closed   atomic.Bool
	inflight sync.WaitGroup
}

// zerologLogger implements Logger using zerolog and optional lumberjack for file output
type zerologLogger struct {
	zl         zerolog.Logger
	closer     io.Closer
	closeOnce  *sync.Once
	exitOnce   *sync.Once
	state      *loggerState
	exitFunc   func(int)
	extractors []ContextExtractor
}

// chainCloser closes a sequence of io.Closer values in order, returning the first error encountered
// Used when both a diode writer and an underlying file writer must be closed: diode first to flush, then file
type chainCloser struct {
	closers []io.Closer
}

func (c *chainCloser) Close() error {
	var firstErr error
	for _, cl := range c.closers {
		if cl == nil {
			continue
		}
		if err := cl.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// New builds a Logger from the given options. Defaults are InfoLevel and ConsoleOutput; nil options are ignored
// Use WithLevel, WithOutput, WithFileOptions, WithServiceName, WithExitFunc, WithHooks, WithSampling,
// WithStackTrace, WithAsync, WithContextExtractor, WithCallerSkip, WithWriter, and WithSyncWriter to configure
// When Output is FileOutput or BothOutput and no custom Writer is set, FileOptions.Filename must be non-empty
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
	if o.Writer == nil && (o.Output == FileOutput || o.Output == BothOutput) {
		if o.FileOptions.Filename == "" {
			return nil, ErrEmptyFilename
		}
	}
	if o.StackTrace {
		// reassigning a package-level var in another package is the documented zerolog API for plugging in
		// a stack marshaler; the variable is declared as a hook (`var ErrorStackMarshaler func(error) interface{}`)
		// and zerolog itself sets it via the same assignment in its docs and pkgerrors subpackage README
		zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack //nolint:reassign // documented zerolog plugin point
	}
	output, primaryCloser, externalCloser := buildOutput(o)
	// When async is configured, wrap the output in a diode. The diode's Close flushes pending events
	// and then calls Close on the wrapped writer when it implements io.Closer. To prevent diode from
	// reaching stdout-backed writers (zerolog.ConsoleWriter and *os.File both implement io.Closer in
	// v1.35.x and would close os.Stdout), strip Close on the wrapped writer when primaryCloser is nil.
	// For owned resources (FileOutput, custom Closer writer) we keep Close so diode flushes-then-closes
	// the resource exactly once; primaryCloser is then the diode itself.
	if o.Async != nil {
		base := output
		if primaryCloser == nil {
			if _, ok := base.(io.Closer); ok {
				base = noCloseWriter{w: base}
			}
		}
		dw := diode.NewWriter(base, o.Async.Size, o.Async.PollInterval, o.Async.OnDrop)
		output = dw
		primaryCloser = dw
	}
	zc := zerolog.New(output).With().Timestamp().CallerWithSkipFrameCount(zerolog.CallerSkipFrameCount + callerWrapperFrames + o.CallerSkip)
	if o.ServiceName != "" {
		zc = zc.Str("service", o.ServiceName)
	}
	zl := zc.Logger().Level(convertLogLevel(o.Level))
	for _, h := range o.Hooks {
		zl = zl.Hook(h)
	}
	if o.Sampler != nil {
		zl = zl.Sample(o.Sampler)
	}
	exitFunc := o.ExitFunc
	if exitFunc == nil {
		exitFunc = os.Exit
	}
	closer := buildCloser(primaryCloser, externalCloser)
	return &zerologLogger{
		zl:         zl,
		closer:     closer,
		closeOnce:  &sync.Once{},
		exitOnce:   &sync.Once{},
		state:      &loggerState{},
		exitFunc:   exitFunc,
		extractors: o.Extractors,
	}, nil
}

// buildOutput returns the base writer along with the primary and external closers (either may be nil).
// primaryCloser is the closer the logger owns directly: lumberjack file (FileOutput) or a custom writer
// that implements io.Closer (WithWriter). It is intentionally nil for stdout-backed writers because
// closing os.Stdout would break the test runner and any subsequent program output - zerolog.ConsoleWriter
// and *os.File both implement io.Closer in v1.35.x and would otherwise be picked up incorrectly.
// externalCloser is set only for BothOutput, where the lumberjack file sits inside MultiLevelWriter
// alongside ConsoleWriter; we cannot use MultiLevelWriter.Close because it would also close stdout via
// ConsoleWriter, so the file writer is returned separately and chained into the close sequence directly.
func buildOutput(o *Options) (writer io.Writer, primaryCloser, externalCloser io.Closer) {
	if o.Writer != nil {
		var pc io.Closer
		if c, ok := o.Writer.(io.Closer); ok {
			pc = c
		}
		return o.Writer, pc, nil
	}
	switch o.Output {
	case ConsoleOutput:
		return zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: timeFormat}, nil, nil
	case FileOutput:
		fo := applyFileDefaults(o.FileOptions)
		lj := &lumberjack.Logger{
			Filename:   fo.Filename,
			MaxSize:    fo.MaxSize,
			MaxBackups: fo.MaxBackups,
			MaxAge:     fo.MaxAge,
			Compress:   fo.Compress,
		}
		return lj, lj, nil
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
		return zerolog.MultiLevelWriter(consoleWriter, fileWriter), nil, fileWriter
	default:
		return os.Stdout, nil, nil
	}
}

// noCloseWriter wraps an io.Writer to hide any Close method on the underlying writer
// Used when wrapping an stdout-backed writer (zerolog.ConsoleWriter, *os.File) in the diode async writer:
// diode.Close calls Close on the wrapped writer when it implements io.Closer, which would otherwise
// reach os.Stdout and break the test runner and any subsequent program output
type noCloseWriter struct {
	w io.Writer
}

func (n noCloseWriter) Write(p []byte) (int, error) { return n.w.Write(p) }

// buildCloser returns a single io.Closer that closes the primary writer first (diode or the writer itself)
// and then the external closer if one exists (BothOutput file that is not reachable via the writer's Close).
// Returns nil when nothing needs to be closed
func buildCloser(primary, external io.Closer) io.Closer {
	switch {
	case primary == nil && external == nil:
		return nil
	case primary == nil:
		return external
	case external == nil:
		return primary
	default:
		return &chainCloser{closers: []io.Closer{primary, external}}
	}
}

func (l *zerologLogger) Debug(msg string, fields ...Fields) {
	l.log(l.zl.Debug(), msg, fields...) //nolint:zerologlint // event is consumed by l.log which calls Msg internally
}

func (l *zerologLogger) Info(msg string, fields ...Fields) {
	l.log(l.zl.Info(), msg, fields...) //nolint:zerologlint // event is consumed by l.log which calls Msg internally
}

func (l *zerologLogger) Warn(msg string, fields ...Fields) {
	l.log(l.zl.Warn(), msg, fields...) //nolint:zerologlint // event is consumed by l.log which calls Msg internally
}

func (l *zerologLogger) Error(msg string, fields ...Fields) {
	l.log(l.zl.Error(), msg, fields...) //nolint:zerologlint // event is consumed by l.log which calls Msg internally
}

func (l *zerologLogger) DebugContext(ctx context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Debug(), msg, l.applyExtractors(ctx, fields)...) //nolint:zerologlint // event consumed by l.log
}

func (l *zerologLogger) InfoContext(ctx context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Info(), msg, l.applyExtractors(ctx, fields)...) //nolint:zerologlint // event consumed by l.log
}

func (l *zerologLogger) WarnContext(ctx context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Warn(), msg, l.applyExtractors(ctx, fields)...) //nolint:zerologlint // event consumed by l.log
}

func (l *zerologLogger) ErrorContext(ctx context.Context, msg string, fields ...Fields) {
	l.log(l.zl.Error(), msg, l.applyExtractors(ctx, fields)...) //nolint:zerologlint // event consumed by l.log
}

func (l *zerologLogger) FatalContext(ctx context.Context, msg string, fields ...Fields) {
	l.fatal(msg, l.applyExtractors(ctx, fields))
}

// applyExtractors prepends extracted Fields to the given fields slice; later entries override earlier ones in logUnchecked
// Returns the original slice unchanged when no extractors are configured or all return empty Fields
func (l *zerologLogger) applyExtractors(ctx context.Context, fields []Fields) []Fields {
	if len(l.extractors) == 0 || ctx == nil {
		return fields
	}
	var extracted []Fields
	for _, ex := range l.extractors {
		if ex == nil {
			continue
		}
		f := ex(ctx)
		if len(f) == 0 {
			continue
		}
		extracted = append(extracted, f)
	}
	if len(extracted) == 0 {
		return fields
	}
	out := make([]Fields, 0, len(extracted)+len(fields))
	out = append(out, extracted...)
	out = append(out, fields...)
	return out
}

func (l *zerologLogger) Fatal(msg string, fields ...Fields) {
	l.fatal(msg, fields)
}

// fatal is the shared implementation for Fatal and FatalContext; it adds a consistent wrapper frame
// so both public methods sit at the same call depth as Info/Debug/etc. (3 frames from logUnchecked)
func (l *zerologLogger) fatal(msg string, fields []Fields) {
	// Mark closed so concurrent log() calls drop new events; then drain in-flight writes before
	// emitting the fatal record so it lands after the last live event in chronological order
	l.state.closed.Store(true)
	l.state.inflight.Wait()
	l.logUnchecked(l.zl.WithLevel(zerolog.FatalLevel), msg, fields...)
	if err := l.Close(); err != nil {
		_, _ = os.Stderr.WriteString("logger close: " + err.Error() + "\n")
	}
	l.exitOnce.Do(func() { l.exitFunc(1) })
}

func (l *zerologLogger) WithFields(fields Fields) Logger {
	s := sanitizeFields(fields)
	m := make(map[string]any, len(s))
	maps.Copy(m, s)
	return &zerologLogger{
		zl:         l.zl.With().Fields(m).Logger(),
		closer:     l.closer,
		closeOnce:  l.closeOnce,
		exitOnce:   l.exitOnce,
		state:      l.state,
		exitFunc:   l.exitFunc,
		extractors: l.extractors,
	}
}

// WithError returns a child logger with err attached as the "error" field on every event
// When WithStackTrace was passed to New and err implements StackTrace (e.g. via pkg/errors), a "stack" field
// is also added on every event from the child logger; otherwise only the "error" field is added
//
// Implementation note: this avoids zerolog.Context.Err which has an early-return bug in v1.35.x when the
// global ErrorStackMarshaler is set and the error does not implement StackTrace - the error field would be
// dropped silently. We add the error field directly via Str and emit the stack via the global marshaler ourselves
func (l *zerologLogger) WithError(err error) Logger {
	if isNilInterface(err) {
		return &zerologLogger{
			zl:         l.zl.With().Logger(),
			closer:     l.closer,
			closeOnce:  l.closeOnce,
			exitOnce:   l.exitOnce,
			state:      l.state,
			exitFunc:   l.exitFunc,
			extractors: l.extractors,
		}
	}
	zc := l.zl.With().Str(zerolog.ErrorFieldName, sanitizeMsg(err.Error()))
	if zerolog.ErrorStackMarshaler != nil {
		if stack := zerolog.ErrorStackMarshaler(err); stack != nil {
			switch v := stack.(type) {
			case string:
				zc = zc.Str(zerolog.ErrorStackFieldName, v)
			default:
				zc = zc.Interface(zerolog.ErrorStackFieldName, v)
			}
		}
	}
	return &zerologLogger{
		zl:         zc.Logger(),
		closer:     l.closer,
		closeOnce:  l.closeOnce,
		exitOnce:   l.exitOnce,
		state:      l.state,
		exitFunc:   l.exitFunc,
		extractors: l.extractors,
	}
}

func (l *zerologLogger) Close() error {
	if l.closer == nil || l.closeOnce == nil {
		return nil
	}
	var err error
	l.closeOnce.Do(func() {
		l.state.closed.Store(true)
		// Wait for in-flight log() calls to release the writer before we close it
		l.state.inflight.Wait()
		err = l.closer.Close()
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

// log is the lock-free hot path: an atomic check followed by a WaitGroup register so Close can drain
// in-flight writes before closing the writer. The double-check after Add covers the race where Close
// observed counter==0 and proceeded to close the writer between the first Load and the Add
func (l *zerologLogger) log(event *zerolog.Event, msg string, fields ...Fields) {
	if l.state.closed.Load() {
		return
	}
	l.state.inflight.Add(1)
	defer l.state.inflight.Done()
	if l.state.closed.Load() {
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

var _noop Logger = &noopLogger{} //nolint:gochecknoglobals // package-level noop sentinel for zero-value safety

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
