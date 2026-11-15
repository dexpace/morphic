// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

// TestDecodeOptions_LocateMapsEachPrefixToItsLocation holds the text form of a
// mapping: the value is split at the first "=" that follows a "/", so a
// prefix with "=" in no path position and a location naming a URL both read,
// and pairs are separated by commas.
func TestDecodeOptions_LocateMapsEachPrefixToItsLocation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, value string
		want        []openapi.Location
	}{
		{"a directory", "https://example.com/x/=.", []openapi.Location{{Prefix: "https://example.com/x/", Location: "."}}},
		{"a URL", "https://example.com/x/=http://mirror.test/y/",
			[]openapi.Location{{Prefix: "https://example.com/x/", Location: "http://mirror.test/y/"}}},
		{"two pairs", "https://a.test/x/=one,https://b.test/y/=../two", []openapi.Location{
			{Prefix: "https://a.test/x/", Location: "one"}, {Prefix: "https://b.test/y/", Location: "../two"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := openapi.New().DecodeOptions(compilers.OptionSet{Settings: map[string]string{"locate": tc.value}})

			require.NoError(t, err)
			opts, ok := got.(openapi.Options)
			require.True(t, ok)
			assert.Equal(t, tc.want, opts.Locations)
		})
	}

	none, err := openapi.New().DecodeOptions(compilers.OptionSet{})
	require.NoError(t, err)
	assert.Empty(t, none.(openapi.Options).Locations, "nothing is mapped unless the caller writes a mapping")
}

// TestDecodeOptions_LocateRefusesAPrefixThatIsNoURIPrefix holds each mapping a
// prefix match could mistake to an error naming the option: a prefix that is
// not absolute, that ends in no "/" (so "https://e.com/xy/b.yaml" would match
// "https://e.com/x"), that holds a query or a fragment, one written twice, a
// value with no "=" after a "/", and a URL location ending in no "/".
func TestDecodeOptions_LocateRefusesAPrefixThatIsNoURIPrefix(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"not absolute":                 "example.com/x/=.",
		"no final slash":               "https://e.com/x=.",
		"a query":                      "https://e.com/x/?q=.",
		"a fragment":                   "https://e.com/x/#f=.",
		"twice":                        "https://e.com/x/=.,https://e.com/x/=b",
		"twice, one spelt differently": "https://e.com/x/=.,HTTPS://E.com:443/x/=b",
		"no location":                  "https://e.com/x/",
		"an empty location":            "https://e.com/x/=",
		"a URL location, no slash":     "https://e.com/x/=https://o.example/y",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := openapi.New().DecodeOptions(compilers.OptionSet{Settings: map[string]string{"locate": value}})

			require.Error(t, err)
			assert.Contains(t, err.Error(), `"locate"`, "the error names the option")
		})
	}
}

// TestCompile_LocationsRefusesAMappingNoPrefixMatchCanRead holds the same
// refusals at the Go option: a Go error, no document and no diagnostic.
func TestCompile_LocationsRefusesAMappingNoPrefixMatchCanRead(t *testing.T) {
	t.Parallel()
	for name, locations := range map[string][]openapi.Location{
		"not absolute":   {{Prefix: "example.com/x/", Location: "."}},
		"no final slash": {{Prefix: "https://e.com/x", Location: "."}},
		"a query":        {{Prefix: "https://e.com/x/?q", Location: "."}},
		"twice":          {{Prefix: "https://e.com/x/", Location: "."}, {Prefix: "https://e.com/x/", Location: "b"}},
		"a URL location": {{Prefix: "https://e.com/x/", Location: "https://o.example/y"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, diags, err := openapi.New().Compile(t.Context(),
				[]compilers.Source{openapitest.SourceOf("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")},
				compilers.Options{FormatOptions: openapi.Options{Locations: locations}})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "locat", "the error names the option")
			assert.Nil(t, doc)
			assert.Empty(t, diags)
		})
	}
}
