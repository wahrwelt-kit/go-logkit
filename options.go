package logkit

import (
	"context"
	"io"
	"time"

	"github.com/rs/zerolog"
)

// Option configures a logger when passed to New. Nil options are ignored; pass WithLevel, WithOutput, etc
type Option func(*Options)

// ContextExtractor extracts fields from a context for inclusion in log events emitted via *Context methods
// Use with WithContextExtractor to wire request-scoped fields (trace_id, request_id, user_id, tenant_id) from
// context.Context into log events without a manual WithFields chain in every handler
// Should be cheap and non-blocking-it runs on every *Context call. Returning a nil or empty Fields adds nothing
type ContextExtractor func(ctx context.Context) Fields

// Options holds configuration for New. Zero value for Level is treated as InfoLevel; zero Output as ConsoleOutput
// When Output is FileOutput or BothOutput, FileOptions.Filename must be set or New returns ErrEmptyFilename
type Options struct {
	Level       Level              // Minimum log level
	Output      OutputType         // Where to write (console, file, or both); ignored when Writer is set
	FileOptions FileOptions        // Required when Output is FileOutput or BothOutput
	ServiceName string             // Added as "service" field to every event when non-empty
	ExitFunc    func(int)          // Called by Fatal after logging and Close; nil uses os.Exit
	Hooks       []zerolog.Hook     // Optional zerolog hooks; run on every event before write
	Sampler     zerolog.Sampler    // Optional sampler; drops events to reduce volume under high load
	StackTrace  bool               // When true, attaches stack traces from pkg/errors-style errors via WithError
	Async       *AsyncOptions      // Optional lock-free async writer (diode) for high-throughput services
	Extractors  []ContextExtractor // Optional context-to-fields extractors invoked by *Context methods
	CallerSkip  int                // Extra caller frames to skip; non-zero when wrapping logkit in a user-defined adapter
	Writer      io.Writer          // Custom output writer; overrides Output/FileOptions when non-nil
}

// FileOptions configures file output (lumberjack). Filename is required when Output is FileOutput or BothOutput
// Zero MaxSize, MaxBackups, and MaxAge use internal defaults (100 MB, 5 backups, 30 days); set them for explicit rotation
type FileOptions struct {
	Filename   string // Log file path (required for FileOutput or BothOutput)
	MaxSize    int    // Max megabytes per file before rotation; zero uses default (100)
	MaxBackups int    // Max number of old files to keep; zero uses default (5)
	MaxAge     int    // Max days to keep old files; zero uses default (30)
	Compress   bool   // Whether to gzip rotated files
}

// AsyncOptions configures the lock-free diode writer for high-throughput services
// Diode buffers events in a ring; under sustained pressure it drops oldest events instead of blocking the producer
// Set Size to the buffer capacity (e.g. 1000-10000 entries) and PollInterval to how often the consumer drains
// PollInterval of 10ms is a sensible default; smaller intervals reduce drop risk but burn more CPU
// OnDrop is called when events are dropped, with the count of missed events; use it to surface backpressure metrics
// When Async is set, the diode wraps the configured Output (Console/File/Both); the diode writer is closed on Close
// before the underlying file writer to flush pending events
type AsyncOptions struct {
	Size         int              // Ring buffer capacity in events; recommended 1000+
	PollInterval time.Duration    // Consumer drain interval; recommended 10*time.Millisecond
	OnDrop       func(missed int) // Optional callback invoked when events are dropped; nil disables the callback
}

// WithLevel sets the minimum log level. Events below this level are not emitted; unknown values are treated as InfoLevel
func WithLevel(level Level) Option {
	return func(o *Options) {
		o.Level = level
	}
}

// WithOutput sets the output destination. For FileOutput or BothOutput, also set FileOptions via WithFileOptions (Filename required)
// Ignored when WithWriter or WithSyncWriter is also passed
func WithOutput(output OutputType) Option {
	return func(o *Options) {
		o.Output = output
	}
}

// WithFileOptions sets file output options. Required when Output is FileOutput or BothOutput; Filename must be non-empty or New returns ErrEmptyFilename
func WithFileOptions(fo FileOptions) Option {
	return func(o *Options) {
		o.FileOptions = fo
	}
}

// WithServiceName sets the service name; adds a "service" field to every log event. Use for multi-service deployments so logs can be filtered by service in centralized logging
func WithServiceName(name string) Option {
	return func(o *Options) {
		o.ServiceName = name
	}
}

// WithExitFunc sets the function called by Fatal after logging and Close. If nil, Fatal calls os.Exit(1)
// Use in tests to avoid terminating the process, or for graceful shutdown (e.g. run exit in a deferred path after flushing)
func WithExitFunc(fn func(int)) Option {
	return func(o *Options) {
		o.ExitFunc = fn
	}
}

// WithHooks registers zerolog hooks that run on every event before write
// Hooks can mutate the event (e.g. add hostname, container_id, OTel trace_id) or perform side effects
// (e.g. forward Error+ events to Sentry, increment a Prometheus counter per level)
// Multiple calls append; nil hooks are skipped. Hooks run in registration order on the same goroutine that emits the event
func WithHooks(hooks ...zerolog.Hook) Option {
	return func(o *Options) {
		for _, h := range hooks {
			if h != nil {
				o.Hooks = append(o.Hooks, h)
			}
		}
	}
}

// WithSampling installs a zerolog sampler that drops events to reduce volume
// Use to bound log throughput under load; common samplers include &zerolog.BasicSampler{N: 10}
// (1-of-N), &zerolog.BurstSampler{Burst: 5, Period: time.Second, NextSampler: ...}, and zerolog.RandomSampler(N)
// Nil disables sampling. Sampling applies to all levels; use zerolog.LevelSampler for per-level policies
func WithSampling(sampler zerolog.Sampler) Option {
	return func(o *Options) {
		o.Sampler = sampler
	}
}

// WithStackTrace enables stack trace extraction for errors attached via WithError
// Requires errors that implement StackTrace() (e.g. wrapped via github.com/pkg/errors or cockroachdb/errors)
// Standard errors created by errors.New or fmt.Errorf without %w do not carry stacks and produce no stack field
// This option installs github.com/rs/zerolog/pkgerrors.MarshalStack as the global zerolog.ErrorStackMarshaler-
// the marshaler is process-global, so concurrent New calls with different StackTrace settings race on the global
// Configure once at startup to avoid surprises
func WithStackTrace() Option {
	return func(o *Options) {
		o.StackTrace = true
	}
}

// WithAsync wraps the configured output in a lock-free diode writer for high-throughput services
// The diode buffers events and drops oldest under sustained pressure instead of blocking the producer-
// trade event loss for predictable latency. Recommended only for services emitting >50k log events/sec
// Size is the ring buffer capacity; PollInterval is the consumer drain interval (e.g. 10*time.Millisecond)
// OnDrop is called with the number of missed events when dropping occurs; use it to surface a metric
// Close on the logger flushes the diode before closing the underlying file writer
func WithAsync(ao AsyncOptions) Option {
	return func(o *Options) {
		o.Async = &ao
	}
}

// WithContextExtractor registers a function that extracts log Fields from a context.Context
// Used by *Context methods (DebugContext, InfoContext, etc.) to enrich events with request-scoped data
// (trace_id, request_id, user_id) without a manual WithFields chain in every handler
// Multiple extractors compose: extracted Fields are merged left-to-right (later extractors override earlier),
// then user-provided Fields override the extracted set. Nil extractors are skipped
// Extractors must be cheap and non-blocking-they run on every *Context call on the caller goroutine
func WithContextExtractor(fn ContextExtractor) Option {
	return func(o *Options) {
		if fn != nil {
			o.Extractors = append(o.Extractors, fn)
		}
	}
}

// WithCallerSkip adds extra caller frames to skip when reporting the source file:line in events
// Use when wrapping logkit in your own adapter (e.g. an app-level helper that calls logkit.Logger methods)
// so the caller field points to the adapter's caller, not the adapter itself
// Each wrapper function between the user call site and the logkit Logger method adds one frame to skip
func WithCallerSkip(extra int) Option {
	return func(o *Options) {
		o.CallerSkip = extra
	}
}

// WithWriter sets a custom io.Writer as the log output, overriding Output/FileOptions
// The writer must be safe for concurrent Write calls; pass a thread-safe writer or use WithSyncWriter
// to wrap a non-thread-safe writer in zerolog.SyncWriter (mutex-per-write)
// If the writer implements io.Closer, Close on the logger calls Close on the writer
// Nil is ignored so callers can pass a conditionally nil writer without silently clearing a previous setting
func WithWriter(w io.Writer) Option {
	return func(o *Options) {
		if w == nil {
			return
		}
		o.Writer = w
	}
}

// WithSyncWriter sets a custom io.Writer wrapped in zerolog.SyncWriter for thread-safety
// Each Write is serialized through a mutex; use this when the underlying writer is not safe for concurrent use
// (e.g. a file opened directly with os.OpenFile, a network connection, an in-memory buffer)
// For writers already thread-safe (lumberjack, os.Stdout), prefer WithWriter to avoid the extra mutex
// Nil is ignored so callers can pass a conditionally nil writer without silently clearing a previous setting
func WithSyncWriter(w io.Writer) Option {
	return func(o *Options) {
		if w == nil {
			return
		}
		o.Writer = zerolog.SyncWriter(w)
	}
}
