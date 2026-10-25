// Package cache keeps API responses on disk (--cache-dir) with their ETag, so
// that a later run can send If-None-Match and reuse the body on 304.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Entry is one cached response. Only these fields are stored: no request or
// response headers, so the token never reaches the disk.
type Entry struct {
	URL  string          `json:"url"`  // the request URL; also guards against hash collisions
	ETag string          `json:"etag"` // sent back verbatim, weak validators included
	Link string          `json:"link"` // Link header of the 200, for a 304 that has none
	Body json.RawMessage `json:"body"` // the JSON array as received
}

// Store is a directory of entries, one file per URL named after the SHA-256
// of the URL. A nil *Store caches nothing.
type Store struct {
	dir string
}

// Open creates dir if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Get returns the entry for url. A missing, unreadable or damaged file is a
// cache miss, not an error: the request simply goes out unconditionally.
func (s *Store) Get(url string) (Entry, bool) {
	if s == nil {
		return Entry{}, false
	}
	data, err := os.ReadFile(s.path(url))
	if err != nil {
		return Entry{}, false
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil || entry.URL != url || entry.ETag == "" ||
		!json.Valid(entry.Body) {
		return Entry{}, false
	}
	return entry, true
}

// Put stores e under e.URL. The file is written to a temporary name and
// renamed, so an interrupted run never leaves a half-written entry behind.
func (s *Store) Put(e Entry) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encode cache entry: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, ".entry-*.tmp")
	if err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one to report
		return fmt.Errorf("write cache entry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path(e.URL)); err != nil {
		return fmt.Errorf("write cache entry: %w", err)
	}
	return nil
}

func (s *Store) path(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}
