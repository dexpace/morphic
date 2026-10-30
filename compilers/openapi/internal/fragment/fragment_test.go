// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package fragment_test

import (
	"encoding/json/jsontext"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/speakeasy-api/openapi/references"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
)

// readings holds each $ref spelling the tests read, with the pointer it names
// and the spelling the resolver is handed for it.
var readings = []struct {
	name     string
	ref      string
	pointer  jsontext.Pointer
	resolver string
}{
	{"plus is a plus", "#/a+b", "/a+b", "#/a%2Bb"},
	{"escaped plus is a plus", "#/a%2Bb", "/a+b", "#/a%2Bb"},
	{"lower-case escape is a plus", "#/a%2bb", "/a+b", "#/a%2bb"},
	{"escaped space is a space", "#/a%20b", "/a b", "#/a%20b"},
	{"a space stays a space", "#/a b", "/a b", "#/a b"},
	{"decoded once", "#/a%252Bb", "/a%2Bb", "#/a%252Bb"},
	{"every plus", "#/a+b+c%20d", "/a+b+c d", "#/a%2Bb%2Bc%20d"},
	{"adjacent pluses", "#/a++b", "/a++b", "#/a%2B%2Bb"},
	{"undecodable keeps its plus", "#/a+b%zz", "/a+b%zz", "#/a+b%zz"},
	{"trailing space is trimmed", "#/a+b ", "/a+b", "#/a%2Bb "},
	{"leading space is trimmed", "# /a+b", "/a+b", "# /a%2Bb"},
	{"space before the hash is trimmed", " #/a+b", "/a+b", " #/a%2Bb"},
	{"another document", "other.yaml#/x+y", "/x+y", "other.yaml#/x%2By"},
	{"plus in the document part", "a+b.yaml#/c", "/c", "a+b.yaml#/c"},
	{"plus in both parts", "a+b.yaml#/c+d", "/c+d", "a+b.yaml#/c%2Bd"},
	{"no fragment", "other.yaml", "", "other.yaml"},
	{"plus and no fragment", "a+b", "", "a+b"},
	{"bare hash", "#", "", "#"},
	{"anchor", "#a+b", "a+b", "#a%2Bb"},
	{"the resolver stops at a second hash", "#/a+b#c+d", "/a+b", "#/a%2Bb#c+d"},
	{"tilde escapes are kept", "#/~1a+b", "/~1a+b", "#/~1a%2Bb"},
	{"nothing to escape", "#/components/schemas/A", "/components/schemas/A", "#/components/schemas/A"},
	{"empty", "", "", ""},
}

func TestPointer_ReadsPlusAsItself(t *testing.T) {
	t.Parallel()
	for _, tc := range readings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.pointer, fragment.Pointer(tc.ref))
		})
	}
}

// readPart returns the text the resolver decodes: the one between the first
// '#' of ref and the next.
func readPart(ref string) (string, bool) {
	parts := strings.Split(ref, "#")
	if len(parts) < 2 {
		return "", false
	}
	return strings.TrimSpace(parts[1]), true
}

func TestPointer_PartsFromTheResolverOnPlusAlone(t *testing.T) {
	t.Parallel()
	for _, tc := range readings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			library := jsontext.Pointer(references.Reference(tc.ref).GetJSONPointer())
			part, found := readPart(tc.ref)
			decoded, err := url.PathUnescape(part)
			if !found || err != nil || !strings.Contains(part, "+") {
				assert.Equal(t, library, fragment.Pointer(tc.ref), "the resolver's own reading")
				return
			}
			assert.Equal(t, jsontext.Pointer(decoded), fragment.Pointer(tc.ref), "percent escapes and nothing else")
			assert.NotEqual(t, library, fragment.Pointer(tc.ref), "the resolver reads each '+' as a space")
		})
	}
}

// resolverReads is the pointer the resolver reads from ref.
func resolverReads(ref string) jsontext.Pointer {
	return jsontext.Pointer(references.Reference(ref).GetJSONPointer())
}

// TestForResolver_SpellsOnlyWhatTheResolverMisreads holds ForResolver to the
// resolver's own reading of each spelling: it changes a reference exactly where
// the resolver reads it as something other than the pointer the table names,
// and the resolver reads that pointer from the spelling it hands back. The
// table's resolver column differs from ref on the same rows, which pins today's
// resolver.
func TestForResolver_SpellsOnlyWhatTheResolverMisreads(t *testing.T) {
	t.Parallel()
	for _, tc := range readings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fragment.ForResolver(tc.ref)
			assert.Equal(t, tc.resolver, got)
			assert.Equal(t, got, fragment.ForResolver(got), "a second pass changes nothing")
			assert.Equal(t, fragment.Pointer(tc.ref), fragment.Pointer(got))

			misread := resolverReads(tc.ref) != tc.pointer
			assert.Equal(t, tc.resolver != tc.ref, misread, "the resolver reads this spelling as something else")
			assert.Equal(t, got != tc.ref, misread, "ForResolver spells exactly what the resolver misreads")
			assert.Equal(t, tc.pointer, resolverReads(got), "the resolver reads the pointer from the spelling it is handed")
		})
	}
}

// percentEncoded spells pointer as a fragment with every byte outside a plain
// set percent-encoded, '+', '%', '#' and whitespace included, so a reader that
// decodes escapes and nothing else reads exactly pointer back.
func percentEncoded(pointer jsontext.Pointer) string {
	const plain = "/~$-_.!*'(),;:@&=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	out := make([]byte, 0, len(pointer))
	for i := range len(pointer) {
		b := pointer[i]
		if strings.IndexByte(plain, b) >= 0 {
			out = append(out, b)
			continue
		}
		out = append(out, fmt.Sprintf("%%%02X", b)...)
	}
	return string(out)
}

// TestPointer_AgreesWithTheResolverOnAnEncodedFragment pins that Pointer and
// the resolver read a fragment spelling every special byte as an escape back as
// the same pointer: neither is left a '+' to read apart.
func TestPointer_AgreesWithTheResolverOnAnEncodedFragment(t *testing.T) {
	t.Parallel()
	tests := []jsontext.Pointer{
		"/$defs/a+b",
		"/$defs/50%",
		"/$defs/a#b",
		"/$defs/a b",
		"/$defs/héllo",
	}
	for _, p := range tests {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			ref := "#" + percentEncoded(p)
			assert.Equal(t, p, fragment.Pointer(ref))
			assert.Equal(t, p, jsontext.Pointer(references.Reference(ref).GetJSONPointer()))
		})
	}
}

// TestPointer_ReadsEveryEncodedByte holds the same property for every byte
// value at the start, the middle and the end of a key. The resolver trims the
// fragment before it decodes it, and a '#' ends it, so those are the places a
// byte left unencoded would be lost.
func TestPointer_ReadsEveryEncodedByte(t *testing.T) {
	t.Parallel()
	for b := range 256 {
		for _, key := range []string{string([]byte{byte(b), 'a'}), string([]byte{'a', byte(b), 'b'}), string([]byte{'a', byte(b)})} {
			p := jsontext.Pointer("/$defs/" + key)
			ref := references.Reference("#" + percentEncoded(p))
			require.Equal(t, p, jsontext.Pointer(ref.GetJSONPointer()), "byte 0x%02x in %q", b, key)
			require.Equal(t, p, fragment.Pointer(string(ref)), "byte 0x%02x in %q", b, key)
			require.Empty(t, ref.GetURI(), "byte 0x%02x in %q", b, key)
		}
	}
}

func FuzzForResolver(f *testing.F) {
	for _, tc := range readings {
		f.Add(tc.ref)
	}
	f.Fuzz(func(t *testing.T, ref string) {
		out := fragment.ForResolver(ref)
		require.Equal(t, out, fragment.ForResolver(out), "idempotent")
		require.Equal(t, fragment.Pointer(ref), fragment.Pointer(out))
		require.Equal(t, fragment.Pointer(ref), jsontext.Pointer(references.Reference(out).GetJSONPointer()),
			"the resolver reads Pointer(ref) from the spelling it is handed")

		// Only '+' is spelled otherwise, by two bytes more each, and nothing
		// outside the part the resolver decodes is touched.
		added := strings.Count(ref, "+") - strings.Count(out, "+")
		require.GreaterOrEqual(t, added, 0)
		require.Len(t, out, len(ref)+2*added)
		uri, _, _ := strings.Cut(ref, "#")
		require.True(t, strings.HasPrefix(out, uri), "the document part")
		if parts := strings.Split(ref, "#"); len(parts) > 2 {
			require.True(t, strings.HasSuffix(out, "#"+strings.Join(parts[2:], "#")), "past the second '#'")
		}

		part, found := readPart(ref)
		if decoded, err := url.PathUnescape(part); found && err == nil {
			require.Equal(t, jsontext.Pointer(decoded), fragment.Pointer(ref))
		}
	})
}
