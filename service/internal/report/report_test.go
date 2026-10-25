package report

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestEncode_MatchesSchema(t *testing.T) {
	r := New(time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600)))
	r.Repos = append(r.Repos,
		Succeeded("cli/cli", []Issue{{
			Number: 1, Title: "a <b> & c", Author: "octocat", Labels: []string{"bug"},
			CreatedAt: "2026-01-05T10:00:00Z", URL: "https://github.com/cli/cli/issues/1",
		}}),
		Succeeded("o/empty", nil),
		Failed("example/missing", NotFound),
	)

	var out bytes.Buffer
	if err := Encode(&out, r); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := `{
  "generated_at": "2026-10-14T09:00:00Z",
  "repos": [
    {
      "repo": "cli/cli",
      "error": null,
      "count": 1,
      "issues": [
        {
          "number": 1,
          "title": "a <b> & c",
          "author": "octocat",
          "labels": [
            "bug"
          ],
          "created_at": "2026-01-05T10:00:00Z",
          "url": "https://github.com/cli/cli/issues/1"
        }
      ]
    },
    {
      "repo": "o/empty",
      "error": null,
      "count": 0,
      "issues": []
    },
    {
      "repo": "example/missing",
      "error": "not_found",
      "count": 0,
      "issues": []
    }
  ]
}
`
	if out.String() != want {
		t.Errorf("Encode:\n got:\n%s\n want:\n%s", out.String(), want)
	}
	if !json.Valid(out.Bytes()) {
		t.Error("output is not valid JSON")
	}
}

func TestEncode_EmptyLabelsAreAnArray(t *testing.T) {
	var out bytes.Buffer
	r := New(time.Unix(0, 0))
	r.Repos = append(r.Repos, Succeeded("o/r", []Issue{{Number: 1, Labels: []string{}}}))
	if err := Encode(&out, r); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"labels": []`)) {
		t.Errorf("labels not written as []:\n%s", out.String())
	}
}

func TestEncodeFatal(t *testing.T) {
	for _, code := range []string{FatalUnauthorized, FatalAllFailed} {
		var out bytes.Buffer
		if err := EncodeFatal(&out, code); err != nil {
			t.Fatalf("EncodeFatal: %v", err)
		}
		if want := `{"error":"` + code + `"}` + "\n"; out.String() != want {
			t.Errorf("EncodeFatal(%q) = %q, want %q", code, out.String(), want)
		}
	}
}
