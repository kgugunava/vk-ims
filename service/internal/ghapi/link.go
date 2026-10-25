package ghapi

import (
	"net/url"
	"strings"
)

// NextLink returns the rel="next" target of a Link header (RFC 8288), resolved
// against the URL of the request, or "" if there is none. Example:
//
//	<https://api.github.com/...&page=2>; rel="next", <...&page=7>; rel="last"
func NextLink(header, requestURL string) string {
	for _, part := range splitLinks(header) {
		target, params, ok := strings.Cut(part, ";")
		target = strings.TrimSpace(target)
		if !ok || !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		if !hasRel(params, "next") {
			continue
		}
		return resolve(requestURL, target[1:len(target)-1])
	}
	return ""
}

// splitLinks splits on commas outside angle brackets: a URL may itself
// contain commas.
func splitLinks(header string) []string {
	var parts []string
	depth, start := 0, 0
	for i, r := range header {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, header[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, header[start:])
}

// hasRel reports whether the link parameters contain rel=<want>, quoted or
// not; rel may hold several space-separated values.
func hasRel(params, want string) bool {
	for _, param := range strings.Split(params, ";") {
		name, value, ok := strings.Cut(param, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
			continue
		}
		for _, rel := range strings.Fields(strings.Trim(strings.TrimSpace(value), `"`)) {
			if strings.EqualFold(rel, want) {
				return true
			}
		}
	}
	return false
}

func resolve(base, ref string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseURL.ResolveReference(refURL).String()
}
