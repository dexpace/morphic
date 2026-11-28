// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest_test

import (
	"encoding/json/v2"
	"math"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/manifest"
	"github.com/dexpace/morphic/pathfold"
)

const (
	digestAlpha = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	digestBeta  = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	// escapablePath holds what an HTML- or JavaScript-safe encoder would escape,
	// so a canonical form written raw pins that no such escaping is applied.
	escapablePath = "a&b<c>\u2028\u00e9.go"
)

func validManifest() manifest.Manifest {
	return manifest.Manifest{
		Schema:  manifest.Schema,
		Target:  "go",
		Options: 1,
		Inputs: manifest.Inputs{
			Doc:     "sha256:doc",
			Plan:    "sha256:plan",
			Emitter: "morphic@dev go1.27.4",
			Config:  "sha256:config",
		},
		Files: []manifest.File{
			{Path: "client.go", Digest: digestAlpha},
			{Path: "models/user.go", Digest: digestBeta},
		},
	}
}

func TestManifest_RoundTrips(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name      string
		input     manifest.Manifest
		want      manifest.Manifest
		omitFiles bool
	}{
		{
			name: "sorts files by path",
			input: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: "models/user.go", Digest: digestBeta},
					{Path: "client.go", Digest: digestAlpha},
				},
			},
			want: validManifest(),
		},
		{
			name: "admits hidden, leading double dot, spaces and non-ASCII paths",
			input: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: "\u00e9/x", Digest: digestAlpha},
					{Path: "a b/c", Digest: digestBeta},
					{Path: "..a", Digest: digestAlpha},
					{Path: ".hidden", Digest: digestBeta},
				},
			},
			want: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: "..a", Digest: digestAlpha},
					{Path: ".hidden", Digest: digestBeta},
					{Path: "a b/c", Digest: digestBeta},
					{Path: "\u00e9/x", Digest: digestAlpha},
				},
			},
		},
		{
			name: "admits a reserved name below the root",
			input: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: "docs/" + manifest.PendingTemporaryName, Digest: digestAlpha},
					{Path: "docs/" + manifest.FileName, Digest: digestAlpha},
					{Path: "docs/" + manifest.PendingName, Digest: digestBeta},
				},
			},
			want: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: "docs/" + manifest.PendingTemporaryName, Digest: digestAlpha},
					{Path: "docs/" + manifest.FileName, Digest: digestAlpha},
					{Path: "docs/" + manifest.PendingName, Digest: digestBeta},
				},
			},
		},
		{
			name: "admits a name beginning with a reserved one and one below the root",
			input: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: "sub/" + manifest.FileName, Digest: digestBeta},
					{Path: manifest.FileName + ".bak", Digest: digestAlpha},
				},
			},
			want: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files: []manifest.File{
					{Path: manifest.FileName + ".bak", Digest: digestAlpha},
					{Path: "sub/" + manifest.FileName, Digest: digestBeta},
				},
			},
		},
		{
			name: "omits empty files and decodes to nil",
			input: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files:   []manifest.File{},
			},
			want: manifest.Manifest{
				Schema:  manifest.Schema,
				Target:  "go",
				Options: 1,
				Inputs:  validManifest().Inputs,
				Files:   nil,
			},
			omitFiles: true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			raw, err := manifest.Encode(row.input)
			require.NoError(t, err)
			if row.omitFiles {
				assert.NotContains(t, string(raw), `"files"`)
			}
			got, err := manifest.Decode(raw)
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(row.want, got), "round-trip mismatch (-want +got)")
		})
	}
}

func TestDecode_SortsUnorderedFiles(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "schema": 1,
  "target": "go",
  "options": 1,
  "inputs": {
    "doc": "sha256:doc",
    "plan": "sha256:plan",
    "emitter": "morphic@dev go1.27.4",
    "config": "sha256:config"
  },
  "files": [
    {"path": "models/user.go", "digest": "` + digestBeta + `"},
    {"path": "client.go", "digest": "` + digestAlpha + `"}
  ]
}`)
	got, err := manifest.Decode(raw)
	require.NoError(t, err)
	assert.Empty(t, cmp.Diff(validManifest(), got), "decoded manifest mismatch (-want +got)")
}

func TestEncode_WritesTheCanonicalForm(t *testing.T) {
	t.Parallel()
	valid := validManifest()
	valid.Files = append(valid.Files, manifest.File{Path: escapablePath, Digest: digestBeta})
	first, err := manifest.Encode(valid)
	require.NoError(t, err)
	second, err := manifest.Encode(valid)
	require.NoError(t, err)
	assert.Equal(t, first, second)

	want := `{
  "schema": 1,
  "target": "go",
  "options": 1,
  "inputs": {
    "doc": "sha256:doc",
    "plan": "sha256:plan",
    "emitter": "morphic@dev go1.27.4",
    "config": "sha256:config"
  },
  "files": [
    {
      "path": "` + escapablePath + `",
      "digest": "` + digestBeta + `"
    },
    {
      "path": "client.go",
      "digest": "` + digestAlpha + `"
    },
    {
      "path": "models/user.go",
      "digest": "` + digestBeta + `"
    }
  ]
}
`
	assert.Equal(t, want, string(first))
}

func TestEncode_LeavesItsArgumentAsItWas(t *testing.T) {
	t.Parallel()
	valid := validManifest()
	valid.Files = []manifest.File{
		{Path: "z.go", Digest: digestBeta},
		{Path: "a.go", Digest: digestAlpha},
	}
	_, err := manifest.Encode(valid)
	require.NoError(t, err)
	assert.Equal(t, []manifest.File{
		{Path: "z.go", Digest: digestBeta},
		{Path: "a.go", Digest: digestAlpha},
	}, valid.Files)
}

func TestDecode_Refuses(t *testing.T) {
	t.Parallel()
	marshalRaw := func(t *testing.T, value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		return raw
	}
	withChange := func(t *testing.T, mutate func(*manifest.Manifest)) []byte {
		t.Helper()
		value := validManifest()
		mutate(&value)
		return marshalRaw(t, value)
	}

	type decodeRow struct {
		name         string
		raw          []byte
		wantNewer    bool
		wantContains string
	}

	baseRows := []decodeRow{
		{name: "not json", raw: []byte("not json")},
		{name: "null", raw: []byte("null"), wantContains: "schema is missing"},
		{name: "empty object", raw: []byte("{}"), wantContains: "schema is missing"},
		{name: "null schema", raw: []byte(`{"schema":null,"target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"}}`), wantContains: "schema is missing"},
		{name: "schema 0", raw: withChange(t, func(value *manifest.Manifest) { value.Schema = 0 })},
		{name: "schema -1", raw: withChange(t, func(value *manifest.Manifest) { value.Schema = -1 })},
		{name: "schema string", raw: []byte(`{"schema":"1","target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"}}`)},
		{name: "schema 2 with unknown member", raw: []byte(`{"schema":2,"target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"},"entities":{}}`), wantNewer: true},
		{name: "schema 1 with unknown member", raw: []byte(`{"schema":1,"target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"},"entities":{}}`)},
		{name: "file with unknown member", raw: []byte(`{"schema":1,"target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"},"files":[{"path":"a.go","digest":"` + digestAlpha + `","origins":["x"]}]}`)},
		{name: "duplicate member name", raw: []byte(`{"schema":1,"target":"go","target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"}}`)},
		{name: "invalid UTF-8 in target", raw: []byte("{\"schema\":1,\"target\":\"go\xff\",\"options\":1,\"inputs\":{\"doc\":\"d\",\"plan\":\"p\",\"emitter\":\"e\",\"config\":\"c\"}}")},
		{name: "invalid UTF-8 in file path", raw: []byte("{\"schema\":1,\"target\":\"go\",\"options\":1,\"inputs\":{\"doc\":\"d\",\"plan\":\"p\",\"emitter\":\"e\",\"config\":\"c\"},\"files\":[{\"path\":\"a/\xff\",\"digest\":\"" + digestAlpha + "\"}]}")},
		{name: "empty target", raw: withChange(t, func(value *manifest.Manifest) { value.Target = "" })},
		{name: "options 0", raw: withChange(t, func(value *manifest.Manifest) { value.Options = 0 })},
		{name: "options -1", raw: withChange(t, func(value *manifest.Manifest) { value.Options = -1 })},
		{name: "empty inputs.doc", raw: withChange(t, func(value *manifest.Manifest) { value.Inputs.Doc = "" })},
		{name: "empty inputs.plan", raw: withChange(t, func(value *manifest.Manifest) { value.Inputs.Plan = "" })},
		{name: "empty inputs.emitter", raw: withChange(t, func(value *manifest.Manifest) { value.Inputs.Emitter = "" })},
		{name: "empty inputs.config", raw: withChange(t, func(value *manifest.Manifest) { value.Inputs.Config = "" })},
		{name: "missing inputs member", raw: []byte(`{"schema":1,"target":"go","options":1}`)},
		{name: "duplicate file path", raw: withChange(t, func(value *manifest.Manifest) {
			value.Files = []manifest.File{
				{Path: "client.go", Digest: digestAlpha},
				{Path: "client.go", Digest: digestBeta},
			}
		})},
		{name: "reserved manifest file", raw: withChange(t, func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.FileName
		})},
		{name: "under reserved pending manifest", raw: withChange(t, func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.PendingName + "/x"
		})},
		{name: "reserved pending temporary file", raw: withChange(t, func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.PendingTemporaryName
		})},
	}

	badPaths := slices.Concat(badManifestPaths(), reservedSpellings())
	badDigests := badManifestDigests()
	rows := make([]decodeRow, 0, len(baseRows)+len(badPaths)+len(badDigests))
	rows = append(rows, baseRows...)
	for _, badPath := range badPaths {
		rows = append(rows, decodeRow{
			name: "bad path " + badPath.name,
			raw: withChange(t, func(value *manifest.Manifest) {
				value.Files[0].Path = badPath.value
			}),
		})
	}
	for _, badDigest := range badDigests {
		rows = append(rows, decodeRow{
			name: "bad digest " + badDigest.name,
			raw: withChange(t, func(value *manifest.Manifest) {
				value.Files[0].Digest = badDigest.value
			}),
		})
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.Decode(row.raw)
			require.Error(t, err)
			if row.wantNewer {
				assert.ErrorIs(t, err, manifest.ErrNewerSchema)
			} else {
				assert.NotErrorIs(t, err, manifest.ErrNewerSchema)
			}
			if row.wantContains != "" {
				assert.ErrorContains(t, err, row.wantContains)
			}
		})
	}
}

func TestEncode_RefusesWhatDecodeRefuses(t *testing.T) {
	t.Parallel()
	withChange := func(mutate func(*manifest.Manifest)) manifest.Manifest {
		value := validManifest()
		mutate(&value)
		return value
	}

	type encodeRow struct {
		name  string
		value manifest.Manifest
	}

	baseRows := []encodeRow{
		{name: "schema 0", value: withChange(func(value *manifest.Manifest) { value.Schema = 0 })},
		{name: "schema -1", value: withChange(func(value *manifest.Manifest) { value.Schema = -1 })},
		{name: "schema 2", value: withChange(func(value *manifest.Manifest) { value.Schema = 2 })},
		{name: "empty target", value: withChange(func(value *manifest.Manifest) { value.Target = "" })},
		{name: "options 0", value: withChange(func(value *manifest.Manifest) { value.Options = 0 })},
		{name: "options -1", value: withChange(func(value *manifest.Manifest) { value.Options = -1 })},
		{name: "empty inputs.doc", value: withChange(func(value *manifest.Manifest) { value.Inputs.Doc = "" })},
		{name: "empty inputs.plan", value: withChange(func(value *manifest.Manifest) { value.Inputs.Plan = "" })},
		{name: "empty inputs.emitter", value: withChange(func(value *manifest.Manifest) { value.Inputs.Emitter = "" })},
		{name: "empty inputs.config", value: withChange(func(value *manifest.Manifest) { value.Inputs.Config = "" })},
		{name: "duplicate file path", value: withChange(func(value *manifest.Manifest) {
			value.Files = []manifest.File{
				{Path: "client.go", Digest: digestAlpha},
				{Path: "client.go", Digest: digestBeta},
			}
		})},
		{name: "reserved manifest file", value: withChange(func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.FileName
		})},
		{name: "under reserved pending manifest", value: withChange(func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.PendingName + "/x"
		})},
		{name: "reserved pending temporary file", value: withChange(func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.PendingTemporaryName
		})},
		{name: "invalid UTF-8 in target", value: withChange(func(value *manifest.Manifest) {
			value.Target = "go\xff"
		})},
		{name: "invalid UTF-8 in file path", value: withChange(func(value *manifest.Manifest) {
			value.Files[0].Path = "a/\xff"
		})},
	}

	badPaths := slices.Concat(badManifestPaths(), reservedSpellings())
	badDigests := badManifestDigests()
	rows := make([]encodeRow, 0, len(baseRows)+len(badPaths)+len(badDigests))
	rows = append(rows, baseRows...)
	for _, badPath := range badPaths {
		rows = append(rows, encodeRow{
			name: "bad path " + badPath.name,
			value: withChange(func(value *manifest.Manifest) {
				value.Files[0].Path = badPath.value
			}),
		})
	}
	for _, badDigest := range badDigests {
		rows = append(rows, encodeRow{
			name: "bad digest " + badDigest.name,
			value: withChange(func(value *manifest.Manifest) {
				value.Files[0].Digest = badDigest.value
			}),
		})
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.Encode(row.value)
			assert.Error(t, err)
		})
	}
}

// maxMessageBytes bounds a message quoting an oversized string: each quotes at
// most 64 characters of it.
const maxMessageBytes = 256

// oversized is a string no message may quote whole.
var oversized = strings.Repeat("a", 1<<16)

// TestEncode_QuotesABoundedPrefixOfWhatItRefuses holds each message quoting a
// manifest's path or digest to a bounded prefix of it, so an oversized one
// cannot size the message.
func TestEncode_QuotesABoundedPrefixOfWhatItRefuses(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		mutate func(*manifest.Manifest)
	}{
		{name: "path holding a backslash", mutate: func(value *manifest.Manifest) {
			value.Files[0].Path = oversized + `\`
		}},
		{name: "path not clean", mutate: func(value *manifest.Manifest) { value.Files[0].Path = oversized + "/" }},
		{name: "path not local", mutate: func(value *manifest.Manifest) { value.Files[0].Path = "/" + oversized }},
		{name: "path under a reserved name", mutate: func(value *manifest.Manifest) {
			value.Files[0].Path = manifest.FileName + "/" + oversized
		}},
		{name: "duplicate path", mutate: func(value *manifest.Manifest) {
			value.Files = []manifest.File{{Path: oversized, Digest: digestAlpha}, {Path: oversized, Digest: digestBeta}}
		}},
		{name: "path with a short digest", mutate: func(value *manifest.Manifest) {
			value.Files[0] = manifest.File{Path: oversized, Digest: "sha256:"}
		}},
		{name: "path with a digest holding a non-hex letter", mutate: func(value *manifest.Manifest) {
			value.Files[0] = manifest.File{Path: oversized, Digest: "sha256:" + strings.Repeat("a", 63) + "g"}
		}},
		{name: "long digest", mutate: func(value *manifest.Manifest) { value.Files[0].Digest = "sha256:" + oversized }},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			value := validManifest()
			row.mutate(&value)
			_, err := manifest.Encode(value)
			require.Error(t, err)
			assert.Less(t, len(err.Error()), maxMessageBytes)
		})
	}
}

type namedString struct {
	name  string
	value string
}

func badManifestPaths() []namedString {
	return []namedString{
		{name: "empty", value: ""},
		{name: "absolute", value: "/a"},
		{name: "backslash", value: `a\b`},
		{name: "parent traversal", value: "../a"},
		{name: "inner parent traversal", value: "a/../b"},
		{name: "double slash", value: "a//b"},
		{name: "inner dot segment", value: "a/./b"},
		{name: "trailing slash", value: "a/"},
		{name: "single dot", value: "."},
		{name: "leading dot slash", value: "./a"},
	}
}

// reservedSpellings spells the reserved names as a file system that folds
// case and normalization reads them alike.
func reservedSpellings() []namedString {
	return []namedString{
		{name: "manifest in capitals", value: "MORPHIC.MANIFEST.JSON"},
		{name: "under the pending manifest in title case", value: "Morphic.Manifest.Pending.json/x"},
		{name: "pending temporary file in capitals", value: ".MORPHIC.MANIFEST.PENDING.JSON.TMP"},
		{name: "manifest with a long s", value: "morphic.manife\u017ft.json"},
		{name: "pending temporary file with two long s", value: ".morphic.manife\u017ft.pending.j\u017fon.tmp"},
	}
}

// TestDecode_AllocatesAtMostTwiceALongFirstSegment decodes a manifest whose
// one path is 1 MiB of combining marks, which the reserved-name check would
// fold were its length not bounded, and holds what the decode allocates to
// twice the input, the fewest of three runs. It does not run in parallel,
// since the count is process-wide.
func TestDecode_AllocatesAtMostTwiceALongFirstSegment(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	raw := []byte(`{"schema":1,"target":"go","options":1,"inputs":{"doc":"d","plan":"p","emitter":"e","config":"c"},` +
		`"files":[{"path":"` + strings.Repeat("\u0301", 1<<19) + `","digest":"` + digestAlpha + `"}]}`)
	fewest := uint64(math.MaxUint64)
	for range 3 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := manifest.Decode(raw)
		runtime.ReadMemStats(&after)
		require.NoError(t, err)
		fewest = min(fewest, after.TotalAlloc-before.TotalAlloc)
	}
	assert.LessOrEqual(t, fewest, uint64(2*len(raw)), "bytes allocated decoding %d bytes", len(raw))
}

// TestFold_LeavesEachReservedNameAsWritten pins that each reserved name is its
// own fold, which comparing a path's fold to the name alone relies on.
func TestFold_LeavesEachReservedNameAsWritten(t *testing.T) {
	t.Parallel()
	for _, name := range []string{manifest.FileName, manifest.PendingName, manifest.PendingTemporaryName} {
		assert.Equal(t, name, pathfold.Fold(name))
	}
}

func badManifestDigests() []namedString {
	return []namedString{
		{name: "empty", value: ""},
		{name: "prefix only", value: "sha256:"},
		{name: "63 hex chars", value: "sha256:" + strings.Repeat("a", 63)},
		{name: "65 hex chars", value: "sha256:" + strings.Repeat("a", 65)},
		{name: "64 chars holding a lowercase non-hex letter", value: "sha256:" + strings.Repeat("a", 63) + "g"},
		{name: "64 uppercase hex chars", value: "sha256:" + strings.Repeat("A", 64)},
		{name: "sha1 algorithm", value: "sha1:" + strings.Repeat("a", 40)},
		{name: "missing algorithm prefix", value: strings.Repeat("a", 64)},
	}
}
