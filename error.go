package logkit

import "errors"

var (
	// ErrEmptyFilename is returned by New when Output is FileOutput or BothOutput but FileOptions.Filename is empty
	// Use errors.Is(err, logkit.ErrEmptyFilename) to detect it and prompt the caller to set WithFileOptions with a non-empty Filename
	ErrEmptyFilename = errors.New("logkit: filename required for file output")
)
