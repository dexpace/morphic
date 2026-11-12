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
	"github.com/dexpace/morphic/ir"
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
			"grouping":                "extension",
			"grouping-key":            "x-speakeasy-group",
			"grouping-lists":          "groups",
			"grouping-fallback":       "path-prefix",
			"allow-external-refs":     "true",
			"overlay":                 "patch.yaml",
			"overlay-lax":             "true",
			"nullable-widening":       "false",
			"discriminator-inference": "false",
			"streaming-media":         "application/vnd.acme.frames; charset=utf-8, +frames",
			"streaming-inference":     "false",
			"binary-media":            "image/*, application/vnd.acme.blob, +zip",
			"binary-inference":        "false",
			"max-source-bytes":        "11",
			"max-source-nodes":        "-1",
			"max-enum-members":        "0",
			"max-alias-surplus":       "+13",
			"max-external-documents":  "14",
			"max-reference-expansion": "15",
		},
	})
	require.NoError(t, err)

	opts, ok := got.(openapi.Options)
	require.True(t, ok, "the decoded value must be this compiler's own options type")
	assert.Equal(t, openapi.GroupByExtension, opts.Grouping)
	assert.Equal(t, openapi.GroupingExtension{
		Key: "x-speakeasy-group", Lists: openapi.ListsAreGroups, Fallback: openapi.GroupByPathPrefix,
	}, opts.GroupingExtension)
	assert.True(t, opts.AllowExternalRefs)
	require.NotNil(t, opts.Overlay)
	assert.Equal(t, "patch.yaml", opts.Overlay.Path)
	assert.Nil(t, opts.Overlay.Data, "named for the compile to read, not read")
	assert.True(t, opts.Overlay.Lax)
	assert.True(t, opts.NullableWidening.Disabled, "nullable-widening=false turns the policy off")
	assert.True(t, opts.DiscriminatorInference.Disabled, "discriminator-inference=false turns the policy off")
	assert.Equal(t, openapi.StreamingMedia{
		Disabled:   true,
		MediaTypes: []string{"application/vnd.acme.frames; charset=utf-8", "+frames"},
	}, opts.StreamingMedia, "the list as the Go field reads it, one entry each, and streaming-inference=false turns it off")
	assert.Equal(t, openapi.BinaryMedia{
		Disabled:   true,
		MediaTypes: []string{"image/*", "application/vnd.acme.blob", "+zip"},
	}, opts.BinaryMedia, "the list as the Go field reads it, and binary-inference=false turns it off")
	assert.Equal(t, openapi.Limits{MaxSourceBytes: 11, MaxSourceNodes: -1, MaxAliasSurplus: 13, MaxExternalDocuments: 14,
		MaxReferenceExpansion: 15}, opts.Limits, "each budget as the Go field reads it, 0 left for the default")
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
			`unknown option "gruoping" (known: allow-external-refs, binary-inference, binary-media, ` +
				`discriminator-inference, grouping, grouping-fallback, grouping-key, grouping-lists, ` +
				`max-alias-surplus, max-enum-members, max-external-documents, max-reference-expansion, ` +
				`max-source-bytes, max-source-nodes, nullable-widening, overlay, overlay-lax, streaming-inference, ` +
				`streaming-media)`},
		{"non-integer budget", map[string]string{"max-source-bytes": "64MiB"},
			`"max-source-bytes": want an integer (0 for the default, negative for none), got "64MiB"`},
		{"budget past an int", map[string]string{"max-source-nodes": "99999999999999999999"},
			`"max-source-nodes": want an integer`},
		{"empty budget", map[string]string{"max-external-documents": ""},
			`"max-external-documents": want an integer`},
		{"unknown grouping", map[string]string{"grouping": "alphabetical"},
			`want "tags", "path-prefix" or "extension", got "alphabetical"`},
		// A grouping extension is read by one strategy alone, so naming one beside
		// another is a mistake about which grouping is in force.
		{"a grouping key with no strategy", map[string]string{"grouping-key": "x-group", "grouping-lists": "segments"},
			`options "grouping-key", "grouping-lists" and "grouping-fallback" apply only with "grouping" set to "extension"`},
		{"a grouping fallback beside tags", map[string]string{"grouping": "tags", "grouping-fallback": "tags"},
			`options "grouping-key", "grouping-lists" and "grouping-fallback" apply only with "grouping" set to "extension"`},
		// Conventions disagree on what a list means, so neither half of the pair
		// is taken from the default's.
		{"a grouping key with no list reading", map[string]string{"grouping": "extension", "grouping-key": "x-group"},
			`option "grouping-lists": want "segments" or "groups" beside "grouping-key", ` +
				`as conventions disagree on what a list means, got ""`},
		{"a list reading with no key", map[string]string{"grouping": "extension", "grouping-lists": "groups"},
			`option "grouping-key": want an extension name starting "x-", got ""`},
		{"a grouping key naming no extension", map[string]string{
			"grouping": "extension", "grouping-key": "group", "grouping-lists": "segments",
		}, `option "grouping-key": want an extension name starting "x-", got "group"`},
		{"an unknown list reading", map[string]string{
			"grouping": "extension", "grouping-key": "x-group", "grouping-lists": "paths",
		}, `want "segments" or "groups" beside "grouping-key", as conventions disagree on what a list means, got "paths"`},
		{"a fallback that reads the extension again", map[string]string{"grouping": "extension", "grouping-fallback": "extension"},
			`option "grouping-fallback": want "tags" or "path-prefix", got "extension"`},
		// An empty value would take a default the caller did not ask for.
		{"an empty grouping key", map[string]string{"grouping": "extension", "grouping-key": ""},
			`option "grouping-key": want a value, got an empty one`},
		{"an empty list reading", map[string]string{"grouping": "extension", "grouping-lists": ""},
			`option "grouping-lists": want a value, got an empty one`},
		{"an empty fallback", map[string]string{"grouping": "extension", "grouping-fallback": ""},
			`option "grouping-fallback": want a value, got an empty one`},
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
		{"a media type with two slashes", map[string]string{"streaming-media": "text/plain/x"},
			`"streaming-media": want media types or +suffixes separated by commas, got "text/plain/x"`},
		{"a media type holding a space", map[string]string{"streaming-media": "text/event stream"},
			`"streaming-media": want media types or +suffixes separated by commas, got "text/event stream"`},
		// A malformed parameter leaves the name readable, so only the parse's error
		// refuses it.
		{"a parameter with no value", map[string]string{"streaming-media": "text/event-stream; charset"},
			`"streaming-media": want media types or +suffixes separated by commas, got "text/event-stream; charset"`},
		{"a suffix holding a space", map[string]string{"streaming-media": "+json seq"},
			`"streaming-media": want media types or +suffixes separated by commas, got "+json seq"`},
		{"a suffix holding a slash", map[string]string{"streaming-media": "+json/seq"},
			`"streaming-media": want media types or +suffixes separated by commas, got "+json/seq"`},
		// A media type's suffix starts at its last plus, so these match nothing.
		{"a suffix holding a plus", map[string]string{"streaming-media": "+a+json-seq"},
			`"streaming-media": want media types or +suffixes separated by commas, got "+a+json-seq"`},
		{"a suffix of a plus", map[string]string{"streaming-media": "++"},
			`"streaming-media": want media types or +suffixes separated by commas, got "++"`},
		{"non-boolean policy", map[string]string{"nullable-widening": "maybe"},
			`option "nullable-widening": want a boolean, got "maybe"`},
		{"non-boolean inference", map[string]string{"streaming-inference": "maybe"},
			`option "streaming-inference": want a boolean, got "maybe"`},
		{"entry naming no binary media type", map[string]string{"binary-media": "image/png,pdf"},
			`"binary-media": want media types or +suffixes separated by commas, got "image/png,pdf"`},
		{"empty binary list", map[string]string{"binary-media": ""},
			`"binary-media": want media types or +suffixes separated by commas, got ""`},
		{"non-boolean binary inference", map[string]string{"binary-inference": "maybe"},
			`option "binary-inference": want a boolean, got "maybe"`},
		{"non-boolean discriminator inference", map[string]string{"discriminator-inference": "maybe"},
			`option "discriminator-inference": want a boolean, got "maybe"`},
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

// TestCompile_GroupingExtensionDefaultsAndRefusals pins the Go caller's side of
// the grouping extension: a zero GroupingExtension takes the default key and
// its list reading, a fallback the caller names stays beside them, and one the
// compile cannot read as asked is refused as the text spelling of it is,
// before anything is read.
func TestCompile_GroupingExtensionDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n" +
		"  /a:\n    get: {operationId: a, x-fern-sdk-group-name: [zero-trust, devices], responses: {\"200\": {description: ok}}}\n" +
		"  /b:\n    get: {operationId: b, tags: [bees], responses: {\"200\": {description: ok}}}\n"
	// filed compiles spec under opts and returns each operation's group, by its
	// operationId; the deepest group the spec nests is two levels down.
	filed := func(opts openapi.Options) (map[string]string, error) {
		doc, _, err := openapi.New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)},
			compilers.Options{FormatOptions: opts})
		if err != nil {
			return nil, err
		}
		require.NotNil(t, doc)
		out := map[string]string{}
		for _, top := range doc.Services[0].Groups {
			for _, g := range append([]ir.OperationGroup{top}, top.Groups...) {
				for _, op := range g.Operations {
					out[op.Name.Source] = string(g.ID)
				}
			}
		}
		return out, nil
	}

	got, err := filed(openapi.Options{Grouping: openapi.GroupByExtension})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "g/extension/zero-trust/devices", "b": "g/tags/bees"}, got,
		"the default key, its list read as one path, and an operation stating none grouped by tags")
	got, err = filed(openapi.Options{
		Grouping: openapi.GroupByExtension, GroupingExtension: openapi.GroupingExtension{Fallback: openapi.GroupByPathPrefix},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "g/extension/zero-trust/devices", "b": "g/path-prefix/b"}, got,
		"the default key beside the fallback the caller names")

	for name, tc := range map[string]struct {
		opts    openapi.Options
		wantErr string
	}{
		"beside tags": {
			openapi.Options{GroupingExtension: openapi.DefaultGroupingExtension()},
			`apply only with "grouping" set to "extension"`,
		},
		"a key alone": {
			openapi.Options{Grouping: openapi.GroupByExtension, GroupingExtension: openapi.GroupingExtension{Key: "x-a"}},
			`option "grouping-lists"`,
		},
		"a fallback naming no strategy": {
			openapi.Options{Grouping: openapi.GroupByExtension, GroupingExtension: openapi.GroupingExtension{Fallback: "first"}},
			`option "grouping-fallback"`,
		},
	} {
		_, err := filed(tc.opts)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.wantErr, name)
	}
}

// TestDefaultGroupingExtension_IsWhatTheLoweringApplies pins the exported
// extension to the one a compile reads when Options names none.
func TestDefaultGroupingExtension_IsWhatTheLoweringApplies(t *testing.T) {
	t.Parallel()
	assert.Equal(t, lowering.DefaultGroupingExtension(), openapi.DefaultGroupingExtension())
}

// TestDefaultStreamingMediaTypes_IsWhatTheLoweringApplies pins the exported
// list to the one a compile classifies by when Options names none, so a caller
// extending it starts from what a compile really uses.
func TestDefaultStreamingMediaTypes_IsWhatTheLoweringApplies(t *testing.T) {
	t.Parallel()
	assert.Equal(t, lowering.DefaultStreamingMediaTypes(), openapi.DefaultStreamingMediaTypes())
}

// TestDefaultBinaryMediaTypes_IsWhatTheLoweringApplies pins the exported list
// to the one a compile classifies by when Options names none.
func TestDefaultBinaryMediaTypes_IsWhatTheLoweringApplies(t *testing.T) {
	t.Parallel()
	assert.Equal(t, lowering.DefaultBinaryMediaTypes(), openapi.DefaultBinaryMediaTypes())
}

// TestDefaultExtensionPromotions_IsWhatTheLoweringApplies pins the exported
// mapping to the one a compile promotes by when Options names none.
func TestDefaultExtensionPromotions_IsWhatTheLoweringApplies(t *testing.T) {
	t.Parallel()
	assert.Equal(t, lowering.DefaultExtensionPromotions(), openapi.DefaultExtensionPromotions())
}
