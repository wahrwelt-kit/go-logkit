package logkit

// Option configures a logger when passed to New. Nil options are ignored; pass WithLevel, WithOutput, etc.
type Option func(*Options)

// Options holds configuration for New. Zero value for Level is treated as InfoLevel; zero Output as ConsoleOutput.
// When Output is FileOutput or BothOutput, FileOptions.Filename must be set or New returns ErrEmptyFilename.
type Options struct {
	Level       Level       // Minimum log level.
	Output      OutputType  // Where to write (console, file, or both).
	FileOptions FileOptions // Required when Output is FileOutput or BothOutput.
	ServiceName string      // Added as "service" field to every event when non-empty.
	ExitFunc    func(int)   // Called by Fatal after logging and Close; nil uses os.Exit.
}

// FileOptions configures file output (lumberjack). Filename is required when Output is FileOutput or BothOutput.
// Zero MaxSize, MaxBackups, and MaxAge use internal defaults (100 MB, 5 backups, 30 days); set them for explicit rotation.
type FileOptions struct {
	Filename   string // Log file path (required for FileOutput or BothOutput).
	MaxSize    int    // Max megabytes per file before rotation; zero uses default (100).
	MaxBackups int    // Max number of old files to keep; zero uses default (5).
	MaxAge     int    // Max days to keep old files; zero uses default (30).
	Compress   bool   // Whether to gzip rotated files.
}

// WithLevel sets the minimum log level. Events below this level are not emitted; unknown values are treated as InfoLevel.
func WithLevel(level Level) Option {
	return func(o *Options) {
		o.Level = level
	}
}

// WithOutput sets the output destination. For FileOutput or BothOutput, also set FileOptions via WithFileOptions (Filename required).
func WithOutput(output OutputType) Option {
	return func(o *Options) {
		o.Output = output
	}
}

// WithFileOptions sets file output options. Required when Output is FileOutput or BothOutput; Filename must be non-empty or New returns ErrEmptyFilename.
func WithFileOptions(fo FileOptions) Option {
	return func(o *Options) {
		o.FileOptions = fo
	}
}

// WithServiceName sets the service name; adds a "service" field to every log event. Use for multi-service deployments so logs can be filtered by service in centralized logging.
func WithServiceName(name string) Option {
	return func(o *Options) {
		o.ServiceName = name
	}
}

// WithExitFunc sets the function called by Fatal after logging and Close. If nil, Fatal calls os.Exit(1).
// Use in tests to avoid terminating the process, or for graceful shutdown (e.g. run exit in a deferred path after flushing).
func WithExitFunc(fn func(int)) Option {
	return func(o *Options) {
		o.ExitFunc = fn
	}
}
