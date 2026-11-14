// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind_test

import (
	"math/rand/v2"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// normal parses s and spells it as bind.NormalURI does.
func normal(t *testing.T, s string) string {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err, s)
	return bind.NormalURI(u)
}

// TestNormalURI_IsIdempotentOverGeneratedURLs holds the spelling to a fixed
// point over URLs built from the segments that interact: dot segments in
// their literal and escaped forms, an escaped separator, unreserved escapes,
// the default, an empty and another port, upper-case hosts and schemes, and an
// opaque scheme. A second pass that moves a URL is a spelling two equal URLs
// reach by different numbers of passes.
func TestNormalURI_IsIdempotentOverGeneratedURLs(t *testing.T) {
	t.Parallel()
	segments := []string{"a", ".", "..", "%2e", "%2E%2e", "%2F", "%41", "b%7e"}
	ports := []string{"", ":", ":80", ":443", ":8080"}
	hosts := []string{"h", "H"}
	schemes := []string{"http", "HTTPS", "https"}
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		var b strings.Builder
		b.WriteString(schemes[r.IntN(len(schemes))] + "://" + hosts[r.IntN(len(hosts))] + ports[r.IntN(len(ports))])
		for range r.IntN(5) {
			b.WriteString("/" + segments[r.IntN(len(segments))])
		}
		u := b.String()
		if _, err := url.Parse(u); err != nil {
			continue
		}

		once := normal(t, u)

		assert.Equal(t, once, normal(t, once), "%s spells %s, which a second pass moves", u, once)
	}
	for _, u := range []string{"urn:x:%41:b", "urn:x"} {
		assert.Equal(t, normal(t, u), normal(t, normal(t, u)), u)
	}
}

// TestNormalURI_SpellsEachEqualURLOneWay holds the spellings of one resource
// RFC 3986 makes equal to the one text, and the ones it keeps apart to two.
func TestNormalURI_SpellsEachEqualURLOneWay(t *testing.T) {
	t.Parallel()
	same := [][]string{
		{"http://h/b", "http://h/a/%2e%2e/b", "HTTP://H/b", "http://h:80/b", "http://h/./b", "http://h/%62"},
		{"https://h/b", "https://h:443/b", "https://H:/b"},
	}
	for _, group := range same {
		for _, u := range group[1:] {
			assert.Equal(t, normal(t, group[0]), normal(t, u), "%s is %s", u, group[0])
		}
	}
	assert.NotEqual(t, normal(t, "http://h/b"), normal(t, "https://h:80/b"))
	assert.NotEqual(t, normal(t, "http://h/b"), normal(t, "http://h/b?"), "an empty query is kept")
	assert.NotEqual(t, normal(t, "http://h/a/%2Fb"), normal(t, "http://h/a/b"), "an escaped separator is no separator")
}

// TestNormalURI_KeepsWhatEachSchemeWrites holds the spelling to what it does
// not change: a file URI's empty authority or its absence, another scheme's
// empty path, a fragment, an opaque URI's text, and a path with no dot.
func TestNormalURI_KeepsWhatEachSchemeWrites(t *testing.T) {
	t.Parallel()
	for u, want := range map[string]string{
		"file:///x/./y.yaml":  "file:///x/y.yaml",
		"file:/x/y.yaml":      "file:/x/y.yaml",
		"foo://h":             "foo://h",
		"http://h/a#%7efrag":  "http://h/a#~frag",
		"https://h/a/b/..":    "https://h/a/",
		"https://h/a/.":       "https://h/a/",
		"http://h/../../a":    "http://h/a",
		"urn:x:%7e":           "urn:x:~",
		"http://u@H:8080/a?b": "http://u@h:8080/a?b",
	} {
		assert.Equal(t, want, normal(t, u), u)
	}
}
