package openapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
)

// TestDetect_SizeDoesNotDecideTheFormat pins the property key order was still
// deciding: a document that declares both keys names one format, and which of
// them detection reaches first is an artefact of where the cap fell, not of the
// document. The prefix answered on `swagger` and stopped, so the same bytes read
// as swagger@2.0 above the cap and openapi@3.0 below it.
func TestDetect_SizeDoesNotDecideTheFormat(t *testing.T) {
	t.Parallel()
	small := `{"swagger":"2.0","openapi":"3.0.3"}`
	big := `{"swagger":"2.0","pad":"` + flowPad() + `","openapi":"3.0.3"}`
	require.LessOrEqual(t, len(small), maxSniffBytes)
	require.Greater(t, len(big), maxSniffBytes)

	want := compilers.SourceFormat{Name: "openapi", Version: "3.0"}
	for name, src := range map[string]string{"below the cap": small, "above the cap": big} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, _, ok := New().Detect(compilers.Source{Path: "spec.json", Data: []byte(src)})
			assert.True(t, ok)
			assert.Equal(t, want, got, "the same document names the same format at any size")
		})
	}
}

// TestDetect_AValueThatIsNoVersionIsNoDeclaration pins the other half of whose
// bytes these are. A key alone does not declare a format: prose sitting beside
// the word is what a document of another format writes, and claiming it reports
// this compiler's complaint over a file that was never its own.
func TestDetect_AValueThatIsNoVersionIsNoDeclaration(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src string }{
		{"prose beside the key", "openapi: is a format\n"},
		{"a pointer beside the key", "openapi: see the docs\n"},
		{"prose beside swagger", "swagger: yes\n"},
		// Version-shaped at its start and not to its end. A prerelease suffix is
		// the live spelling of this: it names no dialect this compiler serves, and
		// reading only the leading digits would claim one it does not.
		{"a prerelease suffix", "openapi: 3.1.0-rc1\n"},
		{"digits and then a word", "openapi: 3x\n"},
		{"markdown past the cap", "# Notes\n\n" + strings.Repeat("filler text\n", 8000) + "openapi: is a format\n"},
		{"flow style, prose for a version", `{"openapi":"is a format"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "README.md", Data: []byte(tc.src)})
			assert.False(t, ok, "prose beside the word is not a declaration of this format")
			assert.Equal(t, compilers.SourceFormat{}, got)
			assert.Nil(t, codesOf(diags), "another format's file earns no complaint from this one")
		})
	}
}

// TestDetect_DoesNotParseTheWholeDocument pins the cost. Detection answers one
// question about two keys, and it runs before the compiler's size and node
// budgets with no context to cancel it, so a parse here is one the loader has
// not yet agreed to pay for. A scan allocates a handful of times whatever the
// document's size; a parse allocates per node.
func TestDetect_DoesNotParseTheWholeDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString("info:\n  title: T\nfiller:\n")
	for b.Len() < 4<<20 {
		b.WriteString("  - key: " + strings.Repeat("v", 80) + "\n")
	}
	b.WriteString("openapi: 3.1.0\n")
	src := compilers.Source{Path: "big.yaml", Data: []byte(b.String())}

	allocs := testing.AllocsPerRun(3, func() {
		if _, _, ok := New().Detect(src); !ok {
			t.Fatal("the document declares a version this compiler serves")
		}
	})
	assert.Less(t, allocs, 100.0,
		"detection scans for a key; it must not build a tree of the whole document")
}

// TestCompile_AnUnreadableSourceIsADiagnostic pins where the complaint about a
// broken document comes from once detection no longer parses one. It has to stay
// a diagnostic: engine.Run turns a compiler's Go error into its own, and the CLI
// maps that to exit 2 — the code it uses for being invoked wrong — so a spec it
// read would be reported as a misuse of itself.
func TestCompile_AnUnreadableSourceIsADiagnostic(t *testing.T) {
	t.Parallel()
	src := "bad: [unterminated\n" + strings.Repeat("filler: x\n", 8000) + "openapi: 3.1.0\n"
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{{Path: "api.yaml", Data: []byte(src)}}, compilers.Options{})

	require.NoError(t, err, "a document that will not parse is a finding, not a failure of the compiler")
	assert.Nil(t, doc)
	assert.Equal(t, []string{diag.UndecodableSource}, codesOf(diags))
}

// TestScanProbe_ReadsTheVersionBesideTheKey pins what the scan reads, on bytes
// that would defeat a parser. Each case is a document past the cap in one of the
// two styles, or one broken in a way that leaves the declaration legible: the
// scan's whole reason to exist is that it answers where a parse cannot.
func TestScanProbe_ReadsTheVersionBesideTheKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      sniffProbe
	}{
		{"flow style", `{"openapi":"3.1.0"}`, sniffProbe{OpenAPI: "3.1.0"}},
		{"flow style, space around the colon", `{"openapi" : "3.1.0"}`, sniffProbe{OpenAPI: "3.1.0"}},
		{"flow style, broken before the key", `{"a":1,"b" 2,"openapi":"3.1.0"}`, sniffProbe{OpenAPI: "3.1.0"}},
		{"block style", "openapi: 3.1.0\n", sniffProbe{OpenAPI: "3.1.0"}},
		{"block style, quoted", "openapi: \"3.1.0\"\n", sniffProbe{OpenAPI: "3.1.0"}},
		{"block style, single quoted", "openapi: '3.1.0'\n", sniffProbe{OpenAPI: "3.1.0"}},
		{"block style, trailing comment", "openapi: 3.1.0 # the version\n", sniffProbe{OpenAPI: "3.1.0"}},
		{"block style, no trailing newline", "openapi: 3.1.0", sniffProbe{OpenAPI: "3.1.0"}},
		{"both keys, whichever order", `{"swagger":"2.0","openapi":"3.1.0"}`,
			sniffProbe{OpenAPI: "3.1.0", Swagger: "2.0"}},

		// A version that is not a quoted scalar declares no dialect, and must not
		// be read as one by accident.
		{"flow style, non-string version", `{"openapi":3}`, sniffProbe{}},
		{"the name has no colon after it", `{"openapi","3.1.0"}`, sniffProbe{}},
		// Depth is what makes an entry the document's own.
		{"nested one level down", `{"a":{"openapi":"3.1.0"}}`, sniffProbe{}},
		{"a document that opens a sequence", `[{"openapi":"3.1.0"}]`, sniffProbe{}},
		{"block style, indented under another key", "a:\n  openapi: 3.1.0\n", sniffProbe{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, scanProbe([]byte(tc.src)))
		})
	}
}

// TestDetect_TheCapBoundaryReadsTheSameBothWays pins the seam. The cap decides
// which of two readings answers — an exact parse at or below it, a scan above —
// and a document does not change format by growing one byte. The boundary is
// where an off-by-one in the comparison hides, and either reading alone still
// looks right from the other side of it.
func TestDetect_TheCapBoundaryReadsTheSameBothWays(t *testing.T) {
	t.Parallel()
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprintf("cap%+d", delta), func(t *testing.T) {
			t.Parallel()
			head := "openapi: 3.1.0\n"
			src := head + "#" + strings.Repeat("p", maxSniffBytes+delta-len(head)-2) + "\n"
			require.Len(t, src, maxSniffBytes+delta, "the case must sit exactly on the boundary")

			got, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(src)})
			assert.True(t, ok)
			assert.Equal(t, want, got, "one byte of padding does not change what a document declares")
			assert.Nil(t, codesOf(diags))
		})
	}
}

// TestDetect_TheCapDecidesWhichReadingAnswers pins the comparison itself. The
// two readings agree on every document either can read, so a document that both
// can read cannot tell them apart and an off-by-one at the cap hides behind that
// agreement. A broken document is where they differ and must: the parse at or
// below the cap has read the whole thing and can say it is this compiler's and
// unreadable, while the scan above it has read one key and cannot tell a broken
// spec from another format's file, so it declines rather than guess.
func TestDetect_TheCapDecidesWhichReadingAnswers(t *testing.T) {
	t.Parallel()
	broken := func(size int) []byte {
		head := "openapi: [unterminated\n"
		src := head + "#" + strings.Repeat("p", size-len(head)-2) + "\n"
		require.Len(t, src, size)
		return []byte(src)
	}
	cases := []struct {
		name     string
		size     int
		wantCode []string
	}{
		{"at the cap, parsed exactly", maxSniffBytes, []string{diag.UndecodableSource}},
		{"one byte past the cap, scanned", maxSniffBytes + 1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: broken(tc.size)})
			assert.False(t, ok, "neither reading finds a version in a document broken before one")
			assert.Equal(t, compilers.SourceFormat{}, got)
			assert.Equal(t, tc.wantCode, codesOf(diags))
		})
	}
}
