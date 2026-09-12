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

// TestDetect_KeyOrderSurvivesTheCap pins the property key order was still
// deciding: a document that declares both keys names one format, and which of
// them detection reaches first is an artefact of where the cap fell, not of the
// document. The prefix answered on `swagger` and stopped, so the same bytes read
// as swagger@2.0 above the cap and openapi@3.0 below it. It pins that one
// shape; the two readings are held to one answer across the shapes they can
// differ on by TestReadings_AgreeExceptWhereDeclared.
func TestDetect_KeyOrderSurvivesTheCap(t *testing.T) {
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
			assert.Equal(t, want, got, "which key the cap fell after does not decide the format")
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
// two readings agree on most documents either can read — every shape they do
// not is declared in TestReadings_AgreeExceptWhereDeclared — so a document that
// both read alike cannot tell them apart and an off-by-one at the cap hides
// behind that agreement. A broken document is where they differ and must: the parse at or
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

// TestDetect_AByteOrderMarkIsNotAFormat pins the seam at the size it bites. A
// mark before the first byte is invisible to the parse below the cap and was
// fatal to the scan above it, so the same document read as OpenAPI at 64 KiB and
// as nothing at all one byte past — on a file no editor thinks is unusual.
func TestDetect_AByteOrderMarkIsNotAFormat(t *testing.T) {
	t.Parallel()
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}
	for name, body := range map[string]string{
		"block style": "openapi: 3.1.0\n#" + flowPad() + "\n",
		"flow style":  `{"openapi":"3.1.0","pad":"` + flowPad() + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "\xef\xbb\xbf" + body
			require.Greater(t, len(src), maxSniffBytes, "the case must exceed the cap to test the scan")
			got, diags, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte(src)})
			assert.True(t, ok, "a byte-order mark is not part of what a document declares")
			assert.Equal(t, want, got)
			assert.Nil(t, codesOf(diags))
		})
	}
}

// readingsRow is one document read both ways. want is what both readings return
// absent a declared loss; declared is why the scan reads less than the parse, or
// "" for a row that must read the same both ways; scan is what the scan returns
// instead, when declared.
type readingsRow struct {
	name, src string
	want      sniffProbe
	declared  string
	scan      sniffProbe
}

// agreeingReadings are the shapes both readings must answer alike. The document
// markers are here in every position a line scan can get wrong, and the shapes
// the scan used to read that the parse never did.
func agreeingReadings() []readingsRow {
	const v = "3.1.0"
	return []readingsRow{
		{name: "block style", src: "openapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "flow style", src: `{"openapi":"3.1.0"}`, want: sniffProbe{OpenAPI: v}},
		{name: "CRLF line endings", src: "openapi: 3.1.0\r\ninfo: {}\r\n", want: sniffProbe{OpenAPI: v}},
		{name: "a tab after the colon", src: "openapi:\t3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "a byte-order mark, block style", src: "\xef\xbb\xbfopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "a byte-order mark, flow style", src: "\xef\xbb\xbf{\"openapi\":\"3.1.0\"}", want: sniffProbe{OpenAPI: v}},
		{name: "an explicit document start", src: "---\nopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "a comment before the document start", src: "# spec\n---\nopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		// 1.1, because yaml.v3 refuses a 1.2 directive outright and the loader
		// shares that parser: such a document is undecodable at any size, so the
		// two readings cannot disagree on its format.
		{name: "a directive before the document start", src: "%YAML 1.1\n---\nopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "a flow document after the marker", src: "--- {\"openapi\":\"3.1.0\"}\n", want: sniffProbe{OpenAPI: v}},
		{name: "a flow document on the line after the marker", src: "---\n{\"openapi\":\"3.1.0\"}\n", want: sniffProbe{OpenAPI: v}},
		{name: "an end marker after the key", src: "openapi: 3.1.0\n...\n", want: sniffProbe{OpenAPI: v}},
		{name: "a plain scalar opening with dashes", src: "---- : 1\nopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "a key opening with one dash", src: "-x: 1\nopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		{name: "a key opening with one dot", src: ".x: 1\nopenapi: 3.1.0\n", want: sniffProbe{OpenAPI: v}},
		// A root sequence is no mapping, so neither reading finds a key in it:
		// the parse refuses the root, and `- openapi` is not the name.
		{name: "a root sequence", src: "- openapi: 3.1.0\n"},
		// Bytes the compile never parses cannot name its format: load reads the
		// first document only.
		{name: "the key in a second document", src: "kind: Foo\n---\nopenapi: 3.1.0\n"},
		{name: "the key after an end marker", src: "kind: Foo\n...\nopenapi: 3.1.0\n"},
		{name: "the key after a block scalar the marker ends", src: "text: |\n  line\n---\nopenapi: 3.1.0\n"},
		// `openapi:3.1.0` is a plain scalar, not a key: the parse refuses the
		// document for having a string at its root, and the scan reads no entry.
		{name: "no space after the colon", src: "openapi:3.1.0\n"},
		{name: "the name as a value", src: `{"note":"openapi"}`},
	}
}

// declaredReadings are the shapes the scan reads less than the parse, each
// against its reason. A tolerance added to the scan deletes a row from here, and
// a shape it stops reading adds one — neither can happen silently.
func declaredReadings() []readingsRow {
	const v = "3.1.0"
	return []readingsRow{
		{name: "a root merge key", src: "base: &b\n  openapi: 3.1.0\n<<: *b\n", want: sniffProbe{OpenAPI: v},
			declared: "following `<<` means resolving an anchor, which means the parse the cap " +
				"exists to avoid — and one an untrusted source would choose"},
		{name: "a quoted key in block style", src: "\"openapi\": 3.1.0\n", want: sniffProbe{OpenAPI: v},
			declared: "the quoted spelling is how flow style writes every key; admitting it at " +
				"column 0 reopens the guard for zero-indent JSON at every depth"},
		{name: "an unquoted key in flow style", src: "{openapi: 3.1.0}", want: sniffProbe{OpenAPI: v},
			declared: "the flow scan is a JSON lexer, and JSON quotes every key"},
		{name: "an anchor before the version", src: "openapi: &v 3.1.0\n", want: sniffProbe{OpenAPI: v},
			declared: "the scan reads the scalar as written, and `&v 3.1.0` is no version", scan: sniffProbe{OpenAPI: "&v 3.1.0"}},
		{name: "a tag before the version", src: "openapi: !!str 3.1.0\n", want: sniffProbe{OpenAPI: v},
			declared: "the scan reads the scalar as written, and `!!str 3.1.0` is no version", scan: sniffProbe{OpenAPI: "!!str 3.1.0"}},
	}
}

// TestReadings_AgreeExceptWhereDeclared holds the two readings to one answer.
// The cap decides which of them runs, so every shape they disagree on is a
// document that names one format below 64 KiB and another above it — and the
// committed corpus cannot see any of it, its largest spec being a few KiB.
//
// declaredReadings is the whole of the list of shapes the scan does not read;
// every other row must read the same both ways. The two halves are asserted in
// opposite directions, so a declared row whose scan catches up fails until its
// reason is deleted, and an undeclared row the scan stops reading fails until
// one is written.
func TestReadings_AgreeExceptWhereDeclared(t *testing.T) {
	t.Parallel()
	for _, tc := range agreeingReadings() {
		t.Run("agree/"+tc.name, func(t *testing.T) {
			t.Parallel()
			require.Empty(t, tc.declared, "an agreeing row carries no reason")
			parsed, _ := decodeYAML([]byte(tc.src))
			assert.Equal(t, tc.want, parsed, "the parse reads what the row says")
			assert.Equal(t, tc.want, scanProbe([]byte(tc.src)), "the scan reads what the parse reads")
		})
	}
	for _, tc := range declaredReadings() {
		t.Run("declared/"+tc.name, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, tc.declared, "a declared loss carries its reason")
			parsed, err := decodeYAML([]byte(tc.src))
			require.NoError(t, err, "the loss is the scan's alone: the parse reads the document")
			assert.Equal(t, tc.want, parsed, "the parse reads what the row says")
			assert.Equal(t, tc.scan, scanProbe([]byte(tc.src)), "the scan reads what the row declares it reads")
			assert.NotEqual(t, tc.want, tc.scan, "the scan now reads this shape, so the reason (%s) is stale: "+
				"move the row to agreeingReadings", tc.declared)
		})
	}
}
