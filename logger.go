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
	"runtime"
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
	redactedValue     = "[REDACTED]"
	// callerWrapperFrames accounts for runtime.Caller -> callerFromSkip -> log -> public method -> user code.
	// *Context methods inline extractor handling and call log directly, so they share the same depth.
	callerWrapperFrames = 3
	// fatalOnceCallerFrames includes the sync.Once frame used to guarantee a single fatal event.
	fatalOnceCallerFrames = 6
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

// loggerState is shared by zerologLogger and its WithFields/WithError children.
// begin/end form an explicit write gate so Close/Fatal can stop new events and wait
// for in-flight writes without misusing sync.WaitGroup as a concurrent register/drain primitive.
type loggerState struct {
	closed atomic.Bool

	mu       sync.Mutex
	cond     *sync.Cond
	inflight int

	shutdownMu   sync.Mutex
	outputClosed bool
}

func newLoggerState() *loggerState {
	s := &loggerState{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *loggerState) begin() bool {
	if s.closed.Load() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return false
	}
	s.inflight++
	return true
}

func (s *loggerState) end() {
	s.mu.Lock()
	s.inflight--
	if s.inflight == 0 {
		s.cond.Broadcast()
	}
	s.mu.Unlock()
}

func (s *loggerState) closeAndWait() {
	s.closed.Store(true)
	s.mu.Lock()
	for s.inflight > 0 {
		s.cond.Wait()
	}
	s.mu.Unlock()
}

// zerologLogger implements Logger using zerolog and optional lumberjack for file output
type zerologLogger struct {
	zl         zerolog.Logger
	closer     io.Closer
	closeOnce  *sync.Once
	fatalOnce  *sync.Once
	state      *loggerState
	exitFunc   func(int)
	extractors []ContextExtractor
	stackTrace bool
	redaction  *redactionConfig
	callerSkip int
	fatalSkip  int
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

// New builds a Logger from the given options. Defaults are InfoLevel and JSON stdout; nil options are ignored
// Use WithLevel, WithOutput, WithFileOptions, WithServiceName, WithExitFunc, WithHooks, WithSampling,
// WithStackTrace, WithAsync, WithContextExtractor, WithCallerSkip, WithWriter, and WithSyncWriter to configure
// When Output is FileOutput or BothOutput and no custom Writer is set, FileOptions.Filename must be non-empty
// Unknown Level values are treated as InfoLevel. Call Close on the returned logger when done (e.g. defer) to release output
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
	if err := validateOptions(o); err != nil {
		return nil, err
	}
	output, primaryCloser, externalCloser := buildOutput(o)
	output, primaryCloser = wrapAsyncOutput(output, primaryCloser, o.Async)
	zc := zerolog.New(output).With().Timestamp()
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
	redaction := newRedactionConfig(o.SensitiveKeys, o.Redactors)
	return &zerologLogger{
		zl:         zl,
		closer:     closer,
		closeOnce:  &sync.Once{},
		fatalOnce:  &sync.Once{},
		state:      newLoggerState(),
		exitFunc:   exitFunc,
		extractors: o.Extractors,
		stackTrace: o.StackTrace,
		redaction:  redaction,
		callerSkip: callerWrapperFrames + o.CallerSkip,
		fatalSkip:  fatalOnceCallerFrames + o.CallerSkip,
	}, nil
}

func validateOptions(o *Options) error {
	if o.Writer == nil && (o.Output == FileOutput || o.Output == BothOutput) {
		if o.FileOptions.Filename == "" {
			return ErrEmptyFilename
		}
	}
	if o.Async != nil {
		if o.Async.Size <= 0 || o.Async.PollInterval < 0 {
			return ErrInvalidAsyncOptions
		}
	}
	return nil
}

func wrapAsyncOutput(output io.Writer, primaryCloser io.Closer, ao *AsyncOptions) (io.Writer, io.Closer) {
	if ao == nil {
		return output, primaryCloser
	}
	// When async is configured, wrap the output in a diode. The diode's Close flushes pending events
	// and then calls Close on the wrapped writer when it implements io.Closer. To prevent diode from
	// reaching stdout-backed writers (zerolog.ConsoleWriter and *os.File both implement io.Closer in
	// v1.35.x and would close os.Stdout), strip Close on the wrapped writer when primaryCloser is nil.
	// For owned resources (FileOutput, custom Closer writer) we keep Close so diode flushes-then-closes
	// the resource exactly once; primaryCloser is then the diode itself.
	base := output
	if primaryCloser == nil {
		if _, ok := base.(io.Closer); ok {
			base = noCloseWriter{w: base}
		}
	}
	dw := diode.NewWriter(base, ao.Size, ao.PollInterval, ao.OnDrop)
	return dw, dw
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
	if o.Output == ConsoleOutput {
		return os.Stdout, nil, nil
	}
	switch o.Output {
	case PrettyConsoleOutput:
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
		fo := applyFileDefaults(o.FileOptions)
		fileWriter := &lumberjack.Logger{
			Filename:   fo.Filename,
			MaxSize:    fo.MaxSize,
			MaxBackups: fo.MaxBackups,
			MaxAge:     fo.MaxAge,
			Compress:   fo.Compress,
		}
		return zerolog.MultiLevelWriter(os.Stdout, fileWriter), nil, fileWriter
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
// so both public methods point the caller field at the user call site.
func (l *zerologLogger) fatal(msg string, fields []Fields) {
	l.fatalOnce.Do(func() {
		l.state.shutdownMu.Lock()
		defer l.state.shutdownMu.Unlock()
		l.state.closeAndWait()
		if !l.state.outputClosed {
			event := l.zl.WithLevel(zerolog.FatalLevel)
			l.logUncheckedWithCaller(event, msg, callerFromSkip(l.fatalSkip), fields...)
		}
		if err := l.closeOutputLocked(); err != nil {
			_, _ = os.Stderr.WriteString("logger close: " + err.Error() + "\n")
		}
		l.exitFunc(1)
	})
}

func (l *zerologLogger) WithFields(fields Fields) Logger {
	s := l.redaction.sanitizeFields(fields)
	m := make(map[string]any, len(s))
	maps.Copy(m, s)
	return &zerologLogger{
		zl:         l.zl.With().Fields(m).Logger(),
		closer:     l.closer,
		closeOnce:  l.closeOnce,
		fatalOnce:  l.fatalOnce,
		state:      l.state,
		exitFunc:   l.exitFunc,
		extractors: l.extractors,
		stackTrace: l.stackTrace,
		redaction:  l.redaction,
		callerSkip: l.callerSkip,
		fatalSkip:  l.fatalSkip,
	}
}

// WithError returns a child logger with err attached as the "error" field on every event
// When WithStackTrace was passed to New and err implements StackTrace (e.g. via pkg/errors), a "stack" field
// is also added on every event from the child logger; otherwise only the "error" field is added
//
// Implementation note: this avoids zerolog.Context.Err edge cases by adding the error field directly via Str.
// When stack traces are enabled for this logger, pkgerrors.MarshalStack is called without mutating global zerolog state.
func (l *zerologLogger) WithError(err error) Logger {
	if isNilInterface(err) {
		return &zerologLogger{
			zl:         l.zl.With().Logger(),
			closer:     l.closer,
			closeOnce:  l.closeOnce,
			fatalOnce:  l.fatalOnce,
			state:      l.state,
			exitFunc:   l.exitFunc,
			extractors: l.extractors,
			stackTrace: l.stackTrace,
			redaction:  l.redaction,
			callerSkip: l.callerSkip,
			fatalSkip:  l.fatalSkip,
		}
	}
	zc := l.zl.With().Str(zerolog.ErrorFieldName, sanitizeMsg(err.Error()))
	if l.stackTrace {
		if stack := pkgerrors.MarshalStack(err); stack != nil {
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
		fatalOnce:  l.fatalOnce,
		state:      l.state,
		exitFunc:   l.exitFunc,
		extractors: l.extractors,
		stackTrace: l.stackTrace,
		redaction:  l.redaction,
		callerSkip: l.callerSkip,
		fatalSkip:  l.fatalSkip,
	}
}

func (l *zerologLogger) Close() error {
	if l.closeOnce == nil {
		return nil
	}
	var err error
	l.closeOnce.Do(func() {
		l.state.shutdownMu.Lock()
		defer l.state.shutdownMu.Unlock()
		l.state.closeAndWait()
		err = l.closeOutputLocked()
	})
	return err
}

func (l *zerologLogger) closeOutputLocked() error {
	if l.state.outputClosed {
		return nil
	}
	l.state.outputClosed = true
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
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
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

var defaultRedaction = newRedactionConfig(nil, nil) //nolint:gochecknoglobals // immutable package default

var defaultSensitiveKeys = []string{ //nolint:gochecknoglobals // immutable package default
	keyAuthorization,
	keyProxyAuthorization,
	keyCookie,
	keySetCookie,
	keyPassword,
	"passwd",
	"token",
	keySecret,
	keyAPIKey,
	keyAccessToken,
	"refresh_token",
	"id_token",
	"session_id",
	"csrf",
	"csrf_token",
	"xsrf",
	"xsrf_token",
	keyPrivateKey,
}

type redactionConfig struct {
	sensitiveKeys map[string]struct{}
	redactors     []FieldRedactor
}

func newRedactionConfig(extraKeys []string, redactors []FieldRedactor) *redactionConfig {
	keys := make(map[string]struct{}, len(defaultSensitiveKeys)+len(extraKeys))
	for _, key := range defaultSensitiveKeys {
		addSensitiveKey(keys, key)
	}
	for _, key := range extraKeys {
		addSensitiveKey(keys, key)
	}
	cfg := &redactionConfig{sensitiveKeys: keys}
	for _, redactor := range redactors {
		if redactor != nil {
			cfg.redactors = append(cfg.redactors, redactor)
		}
	}
	return cfg
}

func addSensitiveKey(keys map[string]struct{}, key string) {
	normalized := normalizeSensitiveKey(key)
	if normalized != "" {
		keys[normalized] = struct{}{}
	}
}

func sanitizeFields(in Fields) Fields {
	return defaultRedaction.sanitizeFields(in)
}

func (c *redactionConfig) sanitizeFields(in Fields) Fields {
	if c == nil {
		c = defaultRedaction
	}
	out := make(Fields, len(in))
	for k, v := range in {
		key := sanitizeMsg(k)
		if redacted, ok := c.redactField(key, v); ok {
			out[key] = c.sanitizeFieldValue(redacted)
			continue
		}
		out[key] = c.sanitizeFieldValue(v)
	}
	return out
}

func (c *redactionConfig) sanitizeFieldValue(v any) any {
	switch val := v.(type) {
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		return val
	case string:
		return sanitizeMsg(val)
	case error:
		if isNilInterface(val) {
			return nilValue
		}
		return sanitizeMsg(val.Error())
	case fmt.Stringer:
		if isNilInterface(val) {
			return nilValue
		}
		return sanitizeMsg(val.String())
	case json.Marshaler:
		return c.sanitizeJSONMarshaler(val)
	case encoding.TextMarshaler:
		return sanitizeTextMarshaler(val)
	default:
		return sanitizeMsg(fmt.Sprint(v))
	}
}

func (c *redactionConfig) sanitizeJSONMarshaler(val json.Marshaler) any {
	if isNilInterface(val) {
		return nilValue
	}
	b, err := val.MarshalJSON()
	if err != nil {
		return sanitizeMsg(fmt.Sprint(val))
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return sanitizeMsg(string(b))
	}
	redacted := c.redactJSONValue("", decoded)
	out, err := json.Marshal(redacted)
	if err != nil {
		return sanitizeMsg(string(b))
	}
	return json.RawMessage(out)
}

func sanitizeTextMarshaler(val encoding.TextMarshaler) any {
	if isNilInterface(val) {
		return nilValue
	}
	b, err := val.MarshalText()
	if err != nil {
		return sanitizeMsg(fmt.Sprint(val))
	}
	return sanitizeMsg(string(b))
}

func isSensitiveKey(key string) bool {
	return defaultRedaction.isSensitiveKey(key)
}

func (c *redactionConfig) isSensitiveKey(key string) bool {
	if c == nil {
		c = defaultRedaction
	}
	normalized := normalizeSensitiveKey(key)
	if _, ok := c.sensitiveKeys[normalized]; ok {
		return true
	}
	return strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret") ||
		isSensitiveTokenKey(normalized) ||
		strings.Contains(normalized, "privatekey") ||
		strings.Contains(normalized, "sessionid") ||
		strings.Contains(normalized, "csrf") ||
		strings.Contains(normalized, "xsrf") ||
		strings.HasSuffix(normalized, "apikey")
}

func isSensitiveTokenKey(normalized string) bool {
	return normalized == "token" ||
		strings.HasSuffix(normalized, "token") ||
		strings.Contains(normalized, "accesstoken") ||
		strings.Contains(normalized, "refreshtoken") ||
		strings.Contains(normalized, "idtoken") ||
		strings.Contains(normalized, "authtoken") ||
		strings.Contains(normalized, "bearertoken") ||
		strings.Contains(normalized, "jwttoken")
}

func normalizeSensitiveKey(key string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', ' ':
			return -1
		default:
			return r
		}
	}, strings.ToLower(key))
}

func (c *redactionConfig) redactField(key string, value any) (any, bool) {
	for _, redactor := range c.redactors {
		if redacted, ok := redactor(key, value); ok {
			return redacted, true
		}
	}
	if c.isSensitiveKey(key) {
		return redactedValue, true
	}
	return nil, false
}

func (c *redactionConfig) redactJSONValue(key string, value any) any {
	if key != "" {
		if redacted, ok := c.redactField(key, value); ok {
			return c.jsonSafeValue(redacted)
		}
	}
	switch val := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[sanitizeMsg(k)] = c.redactJSONValue(k, v)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = c.redactJSONValue("", item)
		}
		return out
	case string:
		return sanitizeMsg(val)
	default:
		return val
	}
}

func (c *redactionConfig) jsonSafeValue(value any) any {
	switch val := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		return val
	case string:
		return sanitizeMsg(val)
	case json.Marshaler:
		return c.sanitizeJSONMarshaler(val)
	case encoding.TextMarshaler:
		return sanitizeTextMarshaler(val)
	case error:
		if isNilInterface(val) {
			return nilValue
		}
		return sanitizeMsg(val.Error())
	case fmt.Stringer:
		if isNilInterface(val) {
			return nilValue
		}
		return sanitizeMsg(val.String())
	default:
		return sanitizeMsg(fmt.Sprint(value))
	}
}

// log uses the shared write gate so Close/Fatal can stop new events and drain active writes.
func (l *zerologLogger) log(event *zerolog.Event, msg string, fields ...Fields) {
	if !l.state.begin() {
		return
	}
	defer l.state.end()
	l.logUncheckedWithCaller(event, msg, callerFromSkip(l.callerSkip), fields...)
}

// logUncheckedWithCaller writes the event without checking closed; used by log and by Fatal (after which process exits).
// Inline fields are sanitized here; WithFields already sanitizes once when building the zerolog context, so no double sanitization for child-logger fields.
func (l *zerologLogger) logUncheckedWithCaller(event *zerolog.Event, msg, caller string, fields ...Fields) {
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
		s := l.redaction.sanitizeFields(merged)
		m := make(map[string]any, len(s))
		maps.Copy(m, s)
		event.Fields(m)
	}
	if caller != "" {
		event.Str(zerolog.CallerFieldName, caller)
	}
	event.Msg(sanitizeMsg(msg))
}

func callerFromSkip(skip int) string {
	if skip < 0 {
		return ""
	}
	pc, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	return zerolog.CallerMarshalFunc(pc, file, line)
}

func callerFromPC(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.PC == 0 || frame.File == "" {
		return ""
	}
	return zerolog.CallerMarshalFunc(frame.PC, frame.File, frame.Line)
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
