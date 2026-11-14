// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOneEditApart_CountsOneEdit pins each edit oneEditApart admits, on either
// side, and what it does not: no edit, two, a swap of runes that are not
// adjacent, and a length two apart. Runes are compared, not bytes.
func TestOneEditApart_CountsOneEdit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"apiKey", "apiKey", false},
		{"apiKey", "apiKy", true}, {"apiKy", "apiKey", true},
		{"apiKey", "xapiKey", true}, {"apiKey", "apiKeyx", true},
		{"apiKey", "apiKex", true}, {"apiKey", "xpiKey", true},
		{"apiKey", "apiKye", true}, {"apiKey", "paiKey", true},
		{"apiKey", "apKiey", true},
		{"apiKey", "apiXex", false}, {"apiKey", "yiKea p", false}, {"apiKey", "apiyeK", false},
		{"apiKey", "apiKeyxx", false}, {"apiKey", "api", false},
		{"clé1", "cle1", true}, {"clé1", "clé", true},
	} {
		assert.Equal(t, tc.want, oneEditApart(tc.a, tc.b), "%q %q", tc.a, tc.b)
	}
}

// TestNearest_TakesTheFirstReadingThatMatches pins each reading, in its order,
// and the least name within one, so the answer depends on the key and the
// declared names alone.
func TestNearest_TakesTheFirstReadingThatMatches(t *testing.T) {
	t.Parallel()
	ix := nearIndexOf([]string{"apiKey", "token", "bearer_token", "Bearer-Token", "jwt"})
	for key, want := range map[string]string{
		"ApiKey": "apiKey", "api-key": "apiKey", "API_KEY": "apiKey",
		"apikey": "apiKey", "A.P.I.K.E.Y": "apiKey",
		"apiKye": "apiKey", "apiKe": "apiKey", "tokne": "token",
		"bearerToken": "Bearer-Token", "jwk": "", "jw": "",
		"basic": "", "": "", "***": "",
	} {
		got, ok := ix.nearest(key)
		assert.Equal(t, want, got, key)
		assert.Equal(t, want != "", ok, key)
	}
	_, ok := ix.nearest("apiKey")
	assert.False(t, ok, "a declared name is no near miss of itself")

	got, _ := nearIndexOf([]string{"Abc", "aBc"}).nearest("a-bc")
	assert.Equal(t, "aBc", got, "the same words come before the same letters, which Abc has too")
}

// TestNearest_ReadsOneEditWithinItsBounds pins the bounds of the edit reading:
// past maxNearNames declared names, or a key past maxNearKeyBytes, it is
// skipped, while the readings by lookup still answer; at each bound it reads.
func TestNearest_ReadsOneEditWithinItsBounds(t *testing.T) {
	t.Parallel()
	names := []string{"apiKey"}
	for i := len(names); i < maxNearNames; i++ {
		names = append(names, "scheme"+strconv.Itoa(i))
	}
	_, ok := nearIndexOf(names).nearest("apiKye")
	assert.True(t, ok, "at the bound of names")
	many := nearIndexOf(append(names, "extra"))
	_, ok = many.nearest("apiKye")
	assert.False(t, ok, "past it, no edit is read")
	got, _ := many.nearest("api_key")
	assert.Equal(t, "apiKey", got, "and a lookup still is")

	long := strings.Repeat("k", maxNearKeyBytes)
	ix := nearIndexOf([]string{long, long + "kk"})
	_, ok = ix.nearest(long[1:] + "x")
	assert.True(t, ok, "a key at the bound of bytes")
	_, ok = ix.nearest(long + "x")
	assert.False(t, ok, "a key past it")

	_, ok = nearIndexOf([]string{"abcd"}).nearest("abd")
	assert.False(t, ok, "a key shorter than minEditKeyBytes")
	_, ok = nearIndexOf([]string{"abcde"}).nearest("abce")
	assert.True(t, ok, "and one at it")
}
