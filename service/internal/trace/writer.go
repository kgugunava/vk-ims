// Package trace writes the --trace log: one JSON line per HTTP attempt.
package trace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Entry is one attempt. Request headers are deliberately absent: the log
// must never contain the Authorization header.
type Entry struct {
	TS        string  `json:"ts"` // RFC 3339, UTC
	Method    string  `json:"method"`
	URL       string  `json:"url"`
	Status    int     `json:"status"`  // 0 if no response was received
	Attempt   int     `json:"attempt"` // from 1 for each URL
	FromCache bool    `json:"from_cache"`
	Error     *string `json:"error"` // null, or a short reason such as "timeout"
}

// Writer appends entries to a file. A nil *Writer discards them, so callers
// need no special case when --trace is not given.
type Writer struct {
	file *os.File
	err  error // first write error, reported by Close
}

// Create truncates or creates the file at path.
func Create(path string) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create trace file: %w", err)
	}
	return &Writer{file: file}, nil
}

// Record writes one line. Each line goes to the file at once, unbuffered, so
// the log is complete up to the last attempt even if the process is killed.
func (w *Writer) Record(e Entry) {
	if w == nil || w.err != nil {
		return
	}
	// URLs keep & as is rather than &; Encode adds the newline.
	var line bytes.Buffer
	enc := json.NewEncoder(&line)
	enc.SetEscapeHTML(false)
	err := enc.Encode(e)
	if err == nil {
		_, err = w.file.Write(line.Bytes())
	}
	if err != nil {
		w.err = fmt.Errorf("write trace: %w", err)
	}
}

// Close closes the file and returns the first error met while writing.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	closeErr := w.file.Close()
	if w.err != nil {
		return w.err
	}
	if closeErr != nil {
		return fmt.Errorf("close trace file: %w", closeErr)
	}
	return nil
}
