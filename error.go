package logkit

import "errors"

// ErrEmptyFilename is returned by New when Output is FileOutput or BothOutput but FileOptions.Filename is empty
// Use errors.Is(err, logkit.ErrEmptyFilename) to detect it and prompt the caller to set WithFileOptions with a non-empty Filename
var ErrEmptyFilename = errors.New("logkit: filename required for file output")

// ErrInvalidAsyncOptions is returned by New when async output is enabled with invalid options.
// Use errors.Is(err, logkit.ErrInvalidAsyncOptions) to detect it and set a positive AsyncOptions.Size.
var ErrInvalidAsyncOptions = errors.New("logkit: invalid async options")
