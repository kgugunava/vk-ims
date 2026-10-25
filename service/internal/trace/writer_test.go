package trace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriter_OneLinePerEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(path, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	timeout := "timeout"
	w.Record(Entry{TS: "2026-10-14T09:00:01Z", Method: "GET", URL: "https://h/x?a=1&b=<>", Status: 200, Attempt: 1})
	w.Record(Entry{TS: "2026-10-14T09:00:02Z", Method: "GET", URL: "https://h/x", Status: 0, Attempt: 2, Error: &timeout})
	w.Record(Entry{TS: "2026-10-14T09:00:03Z", Method: "GET", URL: "https://h/x", Status: 304, Attempt: 1, FromCache: true})
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ts":"2026-10-14T09:00:01Z","method":"GET","url":"https://h/x?a=1&b=<>","status":200,"attempt":1,"from_cache":false,"error":null}
{"ts":"2026-10-14T09:00:02Z","method":"GET","url":"https://h/x","status":0,"attempt":2,"from_cache":false,"error":"timeout"}
{"ts":"2026-10-14T09:00:03Z","method":"GET","url":"https://h/x","status":304,"attempt":1,"from_cache":true,"error":null}
`
	if string(got) != want {
		t.Errorf("trace file:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestWriter_NilDiscards(t *testing.T) {
	var w *Writer
	w.Record(Entry{Method: "GET"})
	if err := w.Close(); err != nil {
		t.Errorf("Close on nil writer: %v", err)
	}
}

func TestCreate_Fails(t *testing.T) {
	if _, err := Create(filepath.Join(t.TempDir(), "missing", "trace.jsonl")); err == nil {
		t.Error("Create in a missing directory succeeded")
	}
}
