// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLocate_AnAbsoluteURLIsSpelledByRFC3986Normalization holds an absolute
// URL a reference writes to the one spelling RFC 3986 sections 6.2.2 and 6.2.3
// give it, whatever base it is resolved against: unreserved escapes decoded
// before dot segments are removed, the hex of the others in upper case, a
// default or empty port dropped for http and https only, an empty path made
// "/", and an empty query or a host's trailing dot left alone.
func TestLocate_AnAbsoluteURLIsSpelledByRFC3986Normalization(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ uri, want string }{
		{"http://h/a/%2e%2e/b", "http://h/b"},
		{"http://h/a/%2E%2e/b", "http://h/b"},
		{"HTTP://H/%7Ea", "http://h/~a"},
		{"http://h/a/%2Fb", "http://h/a/%2Fb"},
		{"http://h/a/%2fb", "http://h/a/%2Fb"},
		{"http://h:80/a", "http://h/a"},
		{"https://h:443/a", "https://h/a"},
		{"http://h:/a", "http://h/a"},
		{"http://h", "http://h/"},
		{"http://h:8080", "http://h:8080/"},
		{"https://h:80/a", "https://h:80/a"},
		{"http://h:443/a", "http://h:443/a"},
		{"http://h/a?", "http://h/a?"},
		{"http://h./a", "http://h./a"},
	} {
		for _, base := range []string{"/api/spec.yaml", "api/spec.yaml", "https://example.com/v1/a.yaml"} {
			got, ok := Locate(c.uri, base)

			assert.True(t, ok, "%q against %q", c.uri, base)
			assert.Equal(t, c.want, got, "%q against %q", c.uri, base)
		}
	}
}

// TestLocate_NormalizingTwiceChangesNothing holds the spelling to a fixed
// point: locating what Locate returned returns it again, which a dot segment
// an escape uncovers would break.
func TestLocate_NormalizingTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	for _, uri := range []string{
		"http://h/a/%2e%2e/b", "http://h/%2e%2e/%2e%2e/b", "http://h/a/%2e/%2E%2e/b", "HTTPS://H:443", "http://h/a/%252e%252e/b",
	} {
		once, ok := Locate(uri, "/api/spec.yaml")
		assert.True(t, ok, uri)

		twice, ok := Locate(once, "/api/spec.yaml")

		assert.True(t, ok, uri)
		assert.Equal(t, once, twice, uri)
	}
}
