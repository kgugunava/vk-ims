package cache

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "nested", "cache") // created by Open
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store, dir
}

func TestStore_RoundTrip(t *testing.T) {
	store, dir := openStore(t)
	entry := Entry{URL: "https://h/x?page=2", ETag: `W/"abc"`, Link: `<https://h/x?page=3>; rel="next"`, Body: []byte(`[{"number":1}]`)}
	if err := store.Put(entry); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := store.Get(entry.URL)
	if !ok || !reflect.DeepEqual(got, entry) {
		t.Errorf("Get = %+v, %v; want %+v", got, ok, entry)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Ext(files[0].Name()) != ".json" {
		t.Errorf("cache directory holds %v, want one .json file and no temporary files", files)
	}
}

func TestStore_Misses(t *testing.T) {
	store, _ := openStore(t)
	if _, ok := store.Get("https://h/unknown"); ok {
		t.Error("Get of an unknown URL hit")
	}

	entry := Entry{URL: "https://h/x", ETag: `"e"`, Body: []byte(`[]`)}
	if err := store.Put(entry); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"damaged JSON": `{"url":"https://h/x","etag":`,
		"other URL":    `{"url":"https://h/other","etag":"\"e\"","link":"","body":[]}`,
		"no ETag":      `{"url":"https://h/x","etag":"","link":"","body":[]}`,
		"no body":      `{"url":"https://h/x","etag":"\"e\"","link":""}`,
		"empty file":   ``,
	} {
		if err := os.WriteFile(store.path(entry.URL), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := store.Get(entry.URL); ok {
			t.Errorf("%s: Get hit, want a miss", name)
		}
	}
}

func TestStore_OverwritesEntry(t *testing.T) {
	store, _ := openStore(t)
	_ = store.Put(Entry{URL: "u", ETag: `"1"`, Body: []byte(`[1]`)})
	if err := store.Put(Entry{URL: "u", ETag: `"2"`, Body: []byte(`[2]`)}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get("u"); got.ETag != `"2"` || string(got.Body) != `[2]` {
		t.Errorf("Get = %+v, want the second entry", got)
	}
}

func TestStore_NilCachesNothing(t *testing.T) {
	var store *Store
	if err := store.Put(Entry{URL: "u", ETag: `"1"`, Body: []byte(`[]`)}); err != nil {
		t.Errorf("Put on nil store: %v", err)
	}
	if _, ok := store.Get("u"); ok {
		t.Error("Get on nil store hit")
	}
}

func TestOpen_Fails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "cache")); err == nil {
		t.Error("Open under a regular file succeeded")
	}
}
