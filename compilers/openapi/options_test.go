package openapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
)

func TestParse_WrongFormatOptions(t *testing.T) {
	t.Parallel()
	_, _, err := New().Compile(context.Background(),
		[]compilers.Source{openapitest.SourceOf("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n")},
		compilers.Options{FormatOptions: "not-openapi-options"})
	require.Error(t, err, "wrong FormatOptions type is a programmer error")
}

func TestParse_ExplicitOptions(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a/b:\n    get: {operationId: ab, responses: {\"200\": {description: ok}}}\n"
	doc, _, err := New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: Options{Grouping: GroupByPathPrefix}})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "a", doc.Services[0].Groups[0].Name.Source)
}

// TestDecodeOptions_TextReachesEveryField is the other half of #69's answer: a
// caller holding only text — the CLI, which imports no compiler — can set every
// option a Go caller can, including the overlay, whose value is a file the
// caller's own reader loads.
func TestDecodeOptions_TextReachesEveryField(t *testing.T) {
	t.Parallel()
	const overlayDoc = "overlay: 1.0.0\ninfo: {title: O, version: \"1\"}\nactions: []\n"
	read := func(name string) ([]byte, error) {
		if name != "patch.yaml" {
			return nil, fmt.Errorf("no such file %q", name)
		}
		return []byte(overlayDoc), nil
	}

	got, err := New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{
			"grouping":            "path-prefix",
			"allow-external-refs": "true",
			"overlay":             "patch.yaml",
			"overlay-lax":         "true",
		},
		ReadFile: read,
	})
	require.NoError(t, err)

	opts, ok := got.(Options)
	require.True(t, ok, "the decoded value must be this compiler's own options type")
	assert.Equal(t, GroupByPathPrefix, opts.Grouping)
	assert.True(t, opts.AllowExternalRefs)
	require.NotNil(t, opts.Overlay)
	assert.Equal(t, "patch.yaml", opts.Overlay.Path)
	assert.Equal(t, overlayDoc, string(opts.Overlay.Data))
	assert.True(t, opts.Overlay.Lax)
}

func TestDecodeOptions_EmptySetIsDefaults(t *testing.T) {
	t.Parallel()
	got, err := New().DecodeOptions(compilers.OptionSet{})
	require.NoError(t, err)
	assert.Equal(t, Options{}, got)
}

func TestDecodeOptions_Refusals(t *testing.T) {
	t.Parallel()
	readsNothing := func(string) ([]byte, error) { return nil, errors.New("no such file") }
	tests := []struct {
		name     string
		settings map[string]string
		readFile func(string) ([]byte, error)
		wantErr  string
	}{
		{"unknown name", map[string]string{"gruoping": "tags"}, nil,
			`unknown option "gruoping" (known: allow-external-refs, grouping, overlay, overlay-lax)`},
		{"unknown grouping", map[string]string{"grouping": "alphabetical"}, nil,
			`want "tags" or "path-prefix", got "alphabetical"`},
		{"non-boolean", map[string]string{"allow-external-refs": "yes please"}, nil,
			`want a boolean, got "yes please"`},
		{"non-boolean laxness", map[string]string{"overlay": "p.yaml", "overlay-lax": "sort of"},
			readsNothing, `want a boolean, got "sort of"`},
		{"laxness with no overlay", map[string]string{"overlay-lax": "true"}, nil,
			`"overlay-lax" applies only with "overlay"`},
		// An empty path is a value that cannot be honoured, not a way to ask for
		// no overlay. Read as the latter it applies none while the caller believes
		// one was applied — and, with laxness set, blames overlay-lax for an
		// overlay the caller did name.
		{"empty overlay path", map[string]string{"overlay": ""}, readsNothing,
			`"overlay": want a file path, got an empty value`},
		{"empty overlay path with laxness", map[string]string{"overlay": "", "overlay-lax": "true"},
			readsNothing, `"overlay": want a file path, got an empty value`},
		{"no reader for a file", map[string]string{"overlay": "p.yaml"}, nil,
			"names a file and the caller supplied no reader"},
		{"unreadable file", map[string]string{"overlay": "p.yaml"}, readsNothing,
			"no such file"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := New().DecodeOptions(compilers.OptionSet{
				Settings: tc.settings,
				ReadFile: tc.readFile,
			})
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
	read := func(string) ([]byte, error) { return []byte("overlay: 1.0.0\n"), nil }

	strict, err := New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"overlay": "p.yaml", "overlay-lax": "false"},
		ReadFile: read,
	})
	require.NoError(t, err)
	lax, err := New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"overlay-lax": "true", "overlay": "p.yaml"},
		ReadFile: read,
	})
	require.NoError(t, err)

	assert.False(t, strict.(Options).Overlay.Lax)
	assert.True(t, lax.(Options).Overlay.Lax)
}

// TestDecodeOptions_FeedsCompile closes the loop the CLI walks: what
// DecodeOptions returns is what Compile accepts, so a setting is not merely
// parsed but read by the lowering.
func TestDecodeOptions_FeedsCompile(t *testing.T) {
	t.Parallel()
	spec := "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n  /a/b:\n    get: {operationId: ab, tags: [zoo], responses: {\"200\": {description: ok}}}\n"
	formatOpts, err := New().DecodeOptions(compilers.OptionSet{
		Settings: map[string]string{"grouping": "path-prefix"},
	})
	require.NoError(t, err)

	doc, _, err := New().Compile(context.Background(), []compilers.Source{openapitest.SourceOf(spec)},
		compilers.Options{FormatOptions: formatOpts})
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, "a", doc.Services[0].Groups[0].Name.Source,
		"the decoded option must reach the lowering, not merely parse")
}
