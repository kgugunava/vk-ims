package ghapi

import "testing"

func TestNextLink(t *testing.T) {
	const request = "https://api.github.com/repos/o/r/issues?per_page=100"
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			"next among several",
			`<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=7>; rel="last"`,
			"https://api.github.com/x?page=2",
		},
		{
			"next is not first",
			`<https://h/x?page=1>; rel="prev", <https://h/x?page=3>; rel="next"`,
			"https://h/x?page=3",
		},
		{"unquoted rel", `<https://h/x?page=2>; rel=next`, "https://h/x?page=2"},
		{"several rel values", `<https://h/x?page=2>; rel="next last"`, "https://h/x?page=2"},
		{"upper case", `<https://h/x?page=2>; REL="NEXT"`, "https://h/x?page=2"},
		{"extra parameters", `<https://h/x?page=2>; title="t"; rel="next"`, "https://h/x?page=2"},
		{"comma inside the url", `<https://h/x?a=1,2&page=2>; rel="next"`, "https://h/x?a=1,2&page=2"},
		{"spaces and newline", " <https://h/x?page=2>;\n\trel=\"next\" ", "https://h/x?page=2"},
		{"relative url", `</repos/o/r/issues?page=2>; rel="next"`, "https://api.github.com/repos/o/r/issues?page=2"},
		{"only last", `<https://h/x?page=7>; rel="last"`, ""},
		{"empty header", "", ""},
		{"no angle brackets", `https://h/x?page=2; rel="next"`, ""},
		{"nextish rel", `<https://h/x?page=2>; rel="nextpage"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextLink(tt.header, request); got != tt.want {
				t.Errorf("NextLink(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
