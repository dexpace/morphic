// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package manifest_test

import (
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/emitters/manifest"
)

func validPending() manifest.Pending {
	return manifest.Pending{
		Schema: manifest.Schema,
		Target: "go",
		Known: map[string][]string{
			"x.go": {digestAlpha},
		},
		TemporaryFiles: []string{".x.go1"},
	}
}

func TestPending_RoundTrips(t *testing.T) {
	t.Parallel()
	nineteenDigits := strings.Repeat("9", 19)
	rows := []struct {
		name      string
		input     manifest.Pending
		want      manifest.Pending
		omitEmpty bool
	}{
		{
			name: "sorts digests and temporary files and admits valid temporary forms",
			input: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
				Known: map[string][]string{
					"a/x.go": {digestBeta, digestAlpha},
					"x.go":   {digestAlpha},
					"a/404":  {digestBeta},
				},
				TemporaryFiles: []string{
					"a/.x.go123",
					".x.go" + nineteenDigits,
					"a/.4041",
					".morphic.manifest.json42",
				},
			},
			want: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
				Known: map[string][]string{
					"a/404":  {digestBeta},
					"a/x.go": {digestAlpha, digestBeta},
					"x.go":   {digestAlpha},
				},
				TemporaryFiles: []string{
					".morphic.manifest.json42",
					".x.go" + nineteenDigits,
					"a/.4041",
					"a/.x.go123",
				},
			},
		},
		{
			name: "admits a name beginning with a reserved one and one below the root",
			input: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
				Known: map[string][]string{
					manifest.FileName + ".bak": {digestAlpha},
					"sub/" + manifest.FileName: {digestBeta},
				},
			},
			want: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
				Known: map[string][]string{
					manifest.FileName + ".bak": {digestAlpha},
					"sub/" + manifest.FileName: {digestBeta},
				},
			},
		},
		{
			name: "admits a known path past the bound on a reserved name's spelling",
			input: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
				Known:  map[string][]string{strings.Repeat("\u2c65", 126) + ".kt": {digestAlpha}},
			},
			want: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
				Known:  map[string][]string{strings.Repeat("\u2c65", 126) + ".kt": {digestAlpha}},
			},
		},
		{
			// A pending manifest holds each temporary file's name "with those
			// any earlier pending manifest records", while a later generation
			// may hold .a1 as a file of its own (docs/emitters/write-integrate.md,
			// apply step 2), so a temporary file that is a known path stands.
			name: "admits a temporary file that is a known path",
			input: manifest.Pending{
				Schema:         manifest.Schema,
				Target:         "go",
				Known:          map[string][]string{"a": {digestAlpha}, ".a1": {digestBeta}},
				TemporaryFiles: []string{".a1"},
			},
			want: manifest.Pending{
				Schema:         manifest.Schema,
				Target:         "go",
				Known:          map[string][]string{"a": {digestAlpha}, ".a1": {digestBeta}},
				TemporaryFiles: []string{".a1"},
			},
		},
		{
			name: "omits empty known and temporaryFiles",
			input: manifest.Pending{
				Schema:         manifest.Schema,
				Target:         "go",
				Known:          map[string][]string{},
				TemporaryFiles: []string{},
			},
			want: manifest.Pending{
				Schema: manifest.Schema,
				Target: "go",
			},
			omitEmpty: true,
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			raw, err := manifest.EncodePending(row.input)
			require.NoError(t, err)
			if row.omitEmpty {
				assert.NotContains(t, string(raw), `"known"`)
				assert.NotContains(t, string(raw), `"temporaryFiles"`)
			}
			got, err := manifest.DecodePending(raw)
			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(row.want, got), "round-trip mismatch (-want +got)")
		})
	}
}

func TestDecodePending_SortsUnorderedEntries(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "schema": 1,
  "target": "go",
  "known": {
    "x.go": ["` + digestBeta + `", "` + digestAlpha + `"],
    "a.go": ["` + digestAlpha + `"]
  },
  "temporaryFiles": [".x.go2", ".a.go1"]
}`)
	got, err := manifest.DecodePending(raw)
	require.NoError(t, err)
	want := manifest.Pending{
		Schema: manifest.Schema,
		Target: "go",
		Known: map[string][]string{
			"a.go": {digestAlpha},
			"x.go": {digestAlpha, digestBeta},
		},
		TemporaryFiles: []string{".a.go1", ".x.go2"},
	}
	assert.Empty(t, cmp.Diff(want, got), "decoded pending manifest mismatch (-want +got)")
}

func TestEncodePending_WritesTheCanonicalForm(t *testing.T) {
	t.Parallel()
	valid := manifest.Pending{
		Schema: manifest.Schema,
		Target: "go",
		Known: map[string][]string{
			"z.go":        {digestBeta, digestAlpha},
			"a.go":        {digestAlpha},
			escapablePath: {digestBeta},
		},
		TemporaryFiles: []string{".z.go2", ".a.go1", "." + escapablePath + "3"},
	}
	want := `{
  "schema": 1,
  "target": "go",
  "known": {
    "` + escapablePath + `": [
      "` + digestBeta + `"
    ],
    "a.go": [
      "` + digestAlpha + `"
    ],
    "z.go": [
      "` + digestAlpha + `",
      "` + digestBeta + `"
    ]
  },
  "temporaryFiles": [
    ".` + escapablePath + `3",
    ".a.go1",
    ".z.go2"
  ]
}
`
	// Each encoding walks the map afresh, so one in a fixed order could match
	// by chance; enough of them cannot.
	const encodings = 32
	for range encodings {
		got, err := manifest.EncodePending(valid)
		require.NoError(t, err)
		require.Equal(t, want, string(got))
	}
}

func TestEncodePending_LeavesItsArgumentAsItWas(t *testing.T) {
	t.Parallel()
	valid := manifest.Pending{
		Schema: manifest.Schema,
		Target: "go",
		Known: map[string][]string{
			"x.go": {digestBeta, digestAlpha},
		},
		TemporaryFiles: []string{".x.go2", ".x.go1"},
	}
	_, err := manifest.EncodePending(valid)
	require.NoError(t, err)
	assert.Equal(t, []string{digestBeta, digestAlpha}, valid.Known["x.go"])
	assert.Equal(t, []string{".x.go2", ".x.go1"}, valid.TemporaryFiles)
}

func TestDecodePending_Refuses(t *testing.T) {
	t.Parallel()
	marshalRaw := func(t *testing.T, value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		return raw
	}
	withChange := func(t *testing.T, mutate func(*manifest.Pending)) []byte {
		t.Helper()
		value := validPending()
		mutate(&value)
		return marshalRaw(t, value)
	}

	type pendingDecodeRow struct {
		name         string
		raw          []byte
		wantNewer    bool
		wantContains string
	}

	baseRows := []pendingDecodeRow{
		{name: "not json", raw: []byte("not json")},
		{name: "null", raw: []byte("null"), wantContains: "schema is missing"},
		{name: "empty object", raw: []byte("{}"), wantContains: "schema is missing"},
		{name: "null schema", raw: []byte(`{"schema":null,"target":"go"}`), wantContains: "schema is missing"},
		{name: "schema 0", raw: withChange(t, func(value *manifest.Pending) { value.Schema = 0 })},
		{name: "schema -1", raw: withChange(t, func(value *manifest.Pending) { value.Schema = -1 })},
		{name: "schema string", raw: []byte(`{"schema":"1","target":"go"}`)},
		{name: "schema 2 with unknown member", raw: []byte(`{"schema":2,"target":"go","entities":{}}`), wantNewer: true},
		{name: "schema 1 with unknown member", raw: []byte(`{"schema":1,"target":"go","entities":{}}`)},
		{name: "duplicate member name", raw: []byte(`{"schema":1,"target":"go","target":"go"}`)},
		{name: "invalid UTF-8 in target", raw: []byte("{\"schema\":1,\"target\":\"go\xff\"}")},
		{name: "empty target", raw: withChange(t, func(value *manifest.Pending) { value.Target = "" })},
		{name: "known path with empty digest list", raw: []byte(`{"schema":1,"target":"go","known":{"a":[]}}`)},
		{name: "known path with null digest list", raw: []byte(`{"schema":1,"target":"go","known":{"a":null}}`)},
		{name: "duplicate digest at known path", raw: withChange(t, func(value *manifest.Pending) {
			value.Known["x.go"] = []string{digestAlpha, digestAlpha}
		})},
		{name: "bad temporary file path", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"../.x.go1"}
		})},
		{name: "duplicate temporary file", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.go1", ".x.go1"}
		})},
		{name: "temporary file with no dot", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"src/user.go"}
		})},
		{name: "temporary file with no digit", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x"}
		})},
		{name: "temporary file with no name", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".1"}
		})},
		{name: "temporary file in directory not in known", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"a/.x.go1"}
		})},
		{name: "temporary file beside unknown file", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".env1"}
		})},
		{name: "temporary file with 20 digits", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.go" + strings.Repeat("1", 20)}
		})},
		{name: "pending manifest temporary file listed", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{manifest.PendingTemporaryName}
		})},
		{name: "manifest temporary file under subdirectory", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"a/.morphic.manifest.json1"}
		})},
		{name: "reserved manifest file in known", raw: withChange(t, func(value *manifest.Pending) {
			value.Known[manifest.FileName] = []string{digestAlpha}
		})},
		{name: "under reserved pending manifest in known", raw: withChange(t, func(value *manifest.Pending) {
			value.Known[manifest.PendingName+"/x"] = []string{digestAlpha}
		})},
		{name: "reserved pending temporary file in known", raw: withChange(t, func(value *manifest.Pending) {
			value.Known[manifest.PendingTemporaryName] = []string{digestAlpha}
		})},
		{name: "temporary file ending in a letter beside a known path", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.gox"}
		})},
		{name: "temporary file ending below a digit beside a known path", raw: withChange(t, func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.go-"}
		})},
	}

	badPaths := slices.Concat(badManifestPaths(), reservedSpellings())
	badDigests := badManifestDigests()
	rows := make([]pendingDecodeRow, 0, len(baseRows)+len(badPaths)+len(badDigests))
	rows = append(rows, baseRows...)
	for _, badPath := range badPaths {
		rows = append(rows, pendingDecodeRow{
			name: "bad known path " + badPath.name,
			raw: withChange(t, func(value *manifest.Pending) {
				value.Known = map[string][]string{badPath.value: {digestAlpha}}
				value.TemporaryFiles = nil
			}),
		})
	}
	for _, badDigest := range badDigests {
		rows = append(rows, pendingDecodeRow{
			name: "bad digest " + badDigest.name,
			raw: withChange(t, func(value *manifest.Pending) {
				value.Known["x.go"] = []string{badDigest.value}
			}),
		})
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.DecodePending(row.raw)
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

// TestDecodePending_ReportsTheFirstBadKnownPathInByteOrder decodes four bad
// known paths many times over, so a walk of them in map order cannot report
// the first in byte order every time by chance.
func TestDecodePending_ReportsTheFirstBadKnownPathInByteOrder(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema":1,"target":"go","known":{` +
		`"z/../y":["` + digestAlpha + `"],` +
		`"m/../n":["` + digestAlpha + `"],` +
		`"a/../b":["` + digestAlpha + `"],` +
		`"d/../e":["` + digestAlpha + `"]}}`)
	const decodings = 32
	for range decodings {
		_, err := manifest.DecodePending(raw)
		require.ErrorContains(t, err, `"a/../b"`)
	}
}

// TestEncodePending_QuotesABoundedPrefixOfWhatItRefuses holds each message
// quoting a known path or a temporary file to a bounded prefix of it, so an
// oversized one cannot size the message.
func TestEncodePending_QuotesABoundedPrefixOfWhatItRefuses(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name   string
		mutate func(*manifest.Pending)
	}{
		{name: "known path with no digest", mutate: func(value *manifest.Pending) {
			value.Known, value.TemporaryFiles = map[string][]string{oversized: nil}, nil
		}},
		{name: "known path repeating a digest", mutate: func(value *manifest.Pending) {
			value.Known, value.TemporaryFiles = map[string][]string{oversized: {digestAlpha, digestAlpha}}, nil
		}},
		{name: "duplicate temporary file", mutate: func(value *manifest.Pending) {
			value.Known = map[string][]string{oversized: {digestAlpha}}
			value.TemporaryFiles = []string{"." + oversized + "1", "." + oversized + "1"}
		}},
		{name: "temporary file with no dot", mutate: func(value *manifest.Pending) {
			value.TemporaryFiles = []string{oversized}
		}},
		{name: "temporary file beside no known path", mutate: func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"." + oversized}
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			value := validPending()
			row.mutate(&value)
			_, err := manifest.EncodePending(value)
			require.Error(t, err)
			assert.Less(t, len(err.Error()), maxMessageBytes)
		})
	}
}

func TestEncodePending_RefusesWhatDecodePendingRefuses(t *testing.T) {
	t.Parallel()
	withChange := func(mutate func(*manifest.Pending)) manifest.Pending {
		value := validPending()
		mutate(&value)
		return value
	}

	type pendingEncodeRow struct {
		name  string
		value manifest.Pending
	}

	baseRows := []pendingEncodeRow{
		{name: "schema 0", value: withChange(func(value *manifest.Pending) { value.Schema = 0 })},
		{name: "schema -1", value: withChange(func(value *manifest.Pending) { value.Schema = -1 })},
		{name: "schema 2", value: withChange(func(value *manifest.Pending) { value.Schema = 2 })},
		{name: "empty target", value: withChange(func(value *manifest.Pending) { value.Target = "" })},
		{name: "known path with no digest", value: withChange(func(value *manifest.Pending) {
			value.Known["x.go"] = nil
		})},
		{name: "duplicate digest at known path", value: withChange(func(value *manifest.Pending) {
			value.Known["x.go"] = []string{digestAlpha, digestAlpha}
		})},
		{name: "bad temporary file path", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"../.x.go1"}
		})},
		{name: "duplicate temporary file", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.go1", ".x.go1"}
		})},
		{name: "temporary file with no dot", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"src/user.go"}
		})},
		{name: "temporary file with no digit", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x"}
		})},
		{name: "temporary file with no name", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".1"}
		})},
		{name: "temporary file in directory not in known", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"a/.x.go1"}
		})},
		{name: "temporary file beside unknown file", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".env1"}
		})},
		{name: "temporary file with 20 digits", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.go" + strings.Repeat("1", 20)}
		})},
		{name: "temporary file ending in a letter beside a known path", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.gox"}
		})},
		{name: "temporary file ending below a digit beside a known path", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{".x.go-"}
		})},
		{name: "pending manifest temporary file listed", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{manifest.PendingTemporaryName}
		})},
		{name: "manifest temporary file under subdirectory", value: withChange(func(value *manifest.Pending) {
			value.TemporaryFiles = []string{"a/.morphic.manifest.json1"}
		})},
		{name: "reserved manifest file in known", value: withChange(func(value *manifest.Pending) {
			value.Known[manifest.FileName] = []string{digestAlpha}
		})},
		{name: "under reserved pending manifest in known", value: withChange(func(value *manifest.Pending) {
			value.Known[manifest.PendingName+"/x"] = []string{digestAlpha}
		})},
		{name: "reserved pending temporary file in known", value: withChange(func(value *manifest.Pending) {
			value.Known[manifest.PendingTemporaryName] = []string{digestAlpha}
		})},
		{name: "invalid UTF-8 in target", value: withChange(func(value *manifest.Pending) {
			value.Target = "go\xff"
		})},
		{name: "invalid UTF-8 in known path", value: withChange(func(value *manifest.Pending) {
			value.Known = map[string][]string{"a/\xff": {digestAlpha}}
			value.TemporaryFiles = nil
		})},
		{name: "invalid UTF-8 in temporary file", value: withChange(func(value *manifest.Pending) {
			value.Known = map[string][]string{"\xff": {digestAlpha}}
			value.TemporaryFiles = []string{".\xff1"}
		})},
	}

	badPaths := slices.Concat(badManifestPaths(), reservedSpellings())
	badDigests := badManifestDigests()
	rows := make([]pendingEncodeRow, 0, len(baseRows)+len(badPaths)+len(badDigests))
	rows = append(rows, baseRows...)
	for _, badPath := range badPaths {
		rows = append(rows, pendingEncodeRow{
			name: "bad known path " + badPath.name,
			value: withChange(func(value *manifest.Pending) {
				value.Known = map[string][]string{badPath.value: {digestAlpha}}
				value.TemporaryFiles = nil
			}),
		})
	}
	for _, badDigest := range badDigests {
		rows = append(rows, pendingEncodeRow{
			name: "bad digest " + badDigest.name,
			value: withChange(func(value *manifest.Pending) {
				value.Known["x.go"] = []string{badDigest.value}
			}),
		})
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			_, err := manifest.EncodePending(row.value)
			assert.Error(t, err)
		})
	}
}
