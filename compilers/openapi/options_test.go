// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapi_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

func TestCompile_WrongFormatOptions(t *testing.T) {
	t.Parallel()
	_, _, err := openapi.New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")},
		compilers.Options{FormatOptions: "not-openapi-options"})
	require.Error(t, err, "wrong FormatOptions type is a programmer error")
}

func TestCompile_ExplicitOptions(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a/b:\n    get: {operationId: ab, responses: {\"200\": {description: ok}}}\n"
	doc, _, err := openapi.New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: openapi.Options{Grouping: openapi.GroupByPathPrefix}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "a", doc.Services[0].Groups[0].Name.Source)
}

// TestDecodeOptions_TextReachesEveryField is the other half of #69's answer: a
// caller holding only text — the CLI, which imports no compiler — can set every
// option a Go caller can, including the overlay, whose value names a file the
// compile reads through the caller's reader. Decoding reads nothing.
func TestDecodeOptions_TextReachesEveryField(t *testing.T) {
	t.Parallel()

	got, err := openapi.New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{
			"grouping":               "path-prefix",
			"allow-external-refs":    "true",
			"overlay":                "patch.yaml",
			"overlay-lax":            "true",
			"nullable-widening":      "false",
			"streaming-media":        "application/vnd.acme.frames, +frames",
			"max-source-bytes":       "11",
			"max-source-nodes":       "-1",
			"max-enum-members":       "0",
			"max-alias-surplus":      "+13",
			"max-external-documents": "14",
		},
	})
	require.NoError(t, err)

	opts, ok := got.(openapi.Options)
	require.True(t, ok, "the decoded value must be this compiler's own options type")
	assert.Equal(t, openapi.GroupByPathPrefix, opts.Grouping)
	assert.True(t, opts.AllowExternalRefs)
	require.NotNil(t, opts.Overlay)
	assert.Equal(t, "patch.yaml", opts.Overlay.Path)
	assert.Nil(t, opts.Overlay.Data, "named for the compile to read, not read")
	assert.True(t, opts.Overlay.Lax)
	assert.True(t, opts.NullableWidening.Disabled, "nullable-widening=false turns the policy off")
	assert.Equal(t, openapi.StreamingMedia{MediaTypes: []string{"application/vnd.acme.frames", "+frames"}},
		opts.StreamingMedia, "the list as the Go field reads it, one entry each")
	assert.Equal(t, openapi.Limits{MaxSourceBytes: 11, MaxSourceNodes: -1, MaxAliasSurplus: 13, MaxExternalDocuments: 14},
		opts.Limits, "each budget as the Go field reads it, 0 left for the default")
}

// TestLimitOptions_NameEveryBudget holds the text vocabulary to Limits itself:
// each field is set by exactly one option, so a budget added to Limits without
// a name to set it by fails here rather than staying out of a text caller's
// reach.
func TestLimitOptions_NameEveryBudget(t *testing.T) {
	t.Parallel()
	var l openapi.Limits
	fields := map[*int]string{}
	for name, field := range openapi.LimitOptions(&l) {
		_, dup := fields[field]
		assert.False(t, dup, "%s sets a field another option sets", name)
		fields[field] = name
	}
	limits := reflect.ValueOf(&l).Elem()
	require.Len(t, fields, limits.NumField(), "an option for each field, and no other")
	for i := range limits.NumField() {
		field, ok := reflect.TypeAssert[*int](limits.Field(i).Addr())
		require.True(t, ok, "%s is an int budget", limits.Type().Field(i).Name)
		assert.Contains(t, fields, field, "%s has an option", limits.Type().Field(i).Name)
	}
}

func TestDecodeOptions_EmptySetIsDefaults(t *testing.T) {
	t.Parallel()
	got, err := openapi.New().DecodeOptions(compilers.OptionSet{})
	require.NoError(t, err)
	assert.Equal(t, openapi.Options{}, got)
}

func TestDecodeOptions_Refusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settings map[string]string
		wantErr  string
	}{
		{"unknown name", map[string]string{"gruoping": "tags"},
			`unknown option "gruoping" (known: allow-external-refs, grouping, max-alias-surplus, ` +
				`max-enum-members, max-external-documents, max-source-bytes, max-source-nodes, nullable-widening, overlay, ` +
				`overlay-lax, streaming-media)`},
		{"non-integer budget", map[string]string{"max-source-bytes": "64MiB"},
			`"max-source-bytes": want an integer (0 for the default, negative for none), got "64MiB"`},
		{"budget past an int", map[string]string{"max-source-nodes": "99999999999999999999"},
			`"max-source-nodes": want an integer`},
		{"empty budget", map[string]string{"max-external-documents": ""},
			`"max-external-documents": want an integer`},
		{"unknown grouping", map[string]string{"grouping": "alphabetical"},
			`want "tags" or "path-prefix", got "alphabetical"`},
		// An entry that could match nothing is refused: the list replaces the
		// default, so a typo would turn the inference off for every media type.
		{"empty streaming list", map[string]string{"streaming-media": ""},
			`"streaming-media": want media types or +suffixes separated by commas, got ""`},
		{"entry naming no media type", map[string]string{"streaming-media": "text/event-stream,ndjson"},
			`"streaming-media": want media types or +suffixes separated by commas, got "text/event-stream,ndjson"`},
		{"empty entry", map[string]string{"streaming-media": "text/event-stream,"},
			`"streaming-media": want media types or +suffixes separated by commas`},
		{"a bare plus", map[string]string{"streaming-media": "+"},
			`"streaming-media": want media types or +suffixes separated by commas, got "+"`},
		{"a media type with no subtype", map[string]string{"streaming-media": "text/"},
			`"streaming-media": want media types or +suffixes separated by commas, got "text/"`},
		{"a media type with no type", map[string]string{"streaming-media": "/plain"},
			`"streaming-media": want media types or +suffixes separated by commas, got "/plain"`},
		{"non-boolean policy", map[string]string{"nullable-widening": "maybe"},
			`option "nullable-widening": want a boolean, got "maybe"`},
		{"non-boolean", map[string]string{"allow-external-refs": "yes please"},
			`want a boolean, got "yes please"`},
		{"non-boolean laxness", map[string]string{"overlay": "p.yaml", "overlay-lax": "sort of"},
			`want a boolean, got "sort of"`},
		{"laxness with no overlay", map[string]string{"overlay-lax": "true"},
			`"overlay-lax" applies only with "overlay"`},
		// An empty path is a value that cannot be honoured, not a way to ask for
		// no overlay. Read as the latter it applies none while the caller believes
		// one was applied — and, with laxness set, blames overlay-lax for an
		// overlay the caller did name.
		{"empty overlay path", map[string]string{"overlay": ""},
			`"overlay": want a file path, got an empty value`},
		{"empty overlay path with laxness", map[string]string{"overlay": "", "overlay-lax": "true"},
			`"overlay": want a file path, got an empty value`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := openapi.New().DecodeOptions(compilers.OptionSet{Settings: tc.settings})
			require.Error(t, err, "a setting that cannot be honoured must not be ignored")
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Nil(t, got)
		})
	}
}

// TestDecodeOptions_OverlayLaxnessIsOrderIndependent pins that the two settings
// describing one overlay are assembled after both have been read: were they
// applied as they arrived, the laxness would depend on which name sorted first.
func TestDecodeOptions_OverlayLaxnessIsOrderIndependent(t *testing.T) {
	t.Parallel()

	strict, err := openapi.New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"overlay": "p.yaml", "overlay-lax": "false"},
	})
	require.NoError(t, err)
	lax, err := openapi.New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"overlay-lax": "true", "overlay": "p.yaml"},
	})
	require.NoError(t, err)

	assert.False(t, strict.(openapi.Options).Overlay.Lax)
	assert.True(t, lax.(openapi.Options).Overlay.Lax)
}

// TestDecodeOptions_FeedsCompile closes the loop the CLI walks: what
// DecodeOptions returns is what Compile accepts, so a setting is not merely
// parsed but read by the lowering.
func TestDecodeOptions_FeedsCompile(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a/b:\n    get: {operationId: ab, tags: [zoo], responses: {\"200\": {description: ok}}}\n"
	formatOpts, err := openapi.New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"grouping": "path-prefix"},
	})
	require.NoError(t, err)

	doc, _, err := openapi.New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: formatOpts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "a", doc.Services[0].Groups[0].Name.Source,
		"the decoded option must reach the lowering, not merely parse")
}

// TestDefaultStreamingMediaTypes_IsWhatTheLoweringApplies pins the exported
// list to the one a compile classifies by when Options names none, so a caller
// extending it starts from what a compile really uses.
func TestDefaultStreamingMediaTypes_IsWhatTheLoweringApplies(t *testing.T) {
	t.Parallel()
	assert.Equal(t, lowering.DefaultStreamingMediaTypes(), openapi.DefaultStreamingMediaTypes())
}

// TestDefaultExtensionPromotions_IsWhatTheLoweringApplies pins the exported
// mapping to the one a compile promotes by when Options names none.
func TestDefaultExtensionPromotions_IsWhatTheLoweringApplies(t *testing.T) {
	t.Parallel()
	assert.Equal(t, lowering.DefaultExtensionPromotions(), openapi.DefaultExtensionPromotions())
}
