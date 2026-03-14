package logger

import "errors"

// Option configures a logger at construction time.
type Option func(*Options)

// Options holds configuration for New. Zero value uses InfoLevel and ConsoleOutput.
type Options struct {
	Level       Level
	Output      OutputType
	FileOptions FileOptions
}

// FileOptions configures file (or both) output. Used by lumberjack; zero values
// may produce unexpected behavior (e.g. no rotation). Set Filename for file output.
type FileOptions struct {
	Filename   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// ErrEmptyFilename is returned by New when Output is FileOutput or BothOutput
// but FileOptions.Filename is empty.
var ErrEmptyFilename = errors.New("logger: filename required for file output")

// WithLevel sets the minimum log level.
func WithLevel(level Level) Option {
	return func(o *Options) {
		o.Level = level
	}
}

// WithOutput sets the output destination (console, file, or both).
func WithOutput(output OutputType) Option {
	return func(o *Options) {
		o.Output = output
	}
}

// WithFileOptions sets file output options; required when Output is FileOutput or BothOutput.
func WithFileOptions(fo FileOptions) Option {
	return func(o *Options) {
		o.FileOptions = fo
	}
}
