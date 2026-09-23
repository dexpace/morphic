package openapi

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
)

// reading is how detection reads a document: it parses, or it does not and
// Detect says so, or it does not and Detect is silent.
type reading int

const (
	// parses is a document the parse reads, declaring a version or not.
	parses reading = iota
	// unreadable is a document that will not parse and that names neither key
	// as its own, so Detect declines it in silence: its bytes may well be
	// another format's.
	unreadable
	// reported is a document that will not parse and writes a discriminating
	// key where a document declares one, so Detect reports it as this
	// compiler's own and broken.
	reported
)

// declarationRow is one document, the OpenAPI version it declares ("" for
// none), and how detection reads it.
type declarationRow struct {
	name, src string
	want      string
	reads     reading
}

// declarations are the shapes detection is held to. They were a table holding
// two readers to one answer — a parse up to 64 KiB, a byte scan past it — with
// a written reason for every row the two read differently (GitHub #486). The
// parse is the one reader now, so each shape has one answer.
//
// The corpus is kept whole. Most of these rows exist because some reading once
// got them wrong: document markers in every position, flow collections and
// quoted scalars left open across lines, node properties in front of a value,
// block scalars carrying what looks like structure. That a single parser reads
// them correctly is not a reason to stop asking.
//
// Each row says how it reads, not only what it declares. A row declaring
// nothing would otherwise pass on a document that never parsed, and Detect's
// answer to such a document — a complaint, or silence — is part of what the
// row pins.
func declarations() []declarationRow {
	const v = "3.1.0"
	return []declarationRow{
		{name: "block style", src: "openapi: 3.1.0\n", want: v},
		{name: "flow style", src: `{"openapi":"3.1.0"}`, want: v},
		{name: "CRLF line endings", src: "openapi: 3.1.0\r\ninfo: {}\r\n", want: v},
		{name: "a tab after the colon", src: "openapi:\t3.1.0\n", want: v},
		{name: "a tab before a trailing comment", src: "openapi: 3.1.0\t# c\n", want: v},
		{name: "a space before the colon", src: "openapi : 3.1.0\n", want: v},
		{name: "a tab before the colon", src: "openapi\t: 3.1.0\n", want: v},
		{name: "a bare scalar in flow style", src: `{"openapi": 3.1}`, want: "3.1"},
		{name: "a bare scalar closing the flow mapping", src: `{"openapi":3.1}`, want: "3.1"},
		{name: "a bare scalar before a sibling", src: `{"openapi":3.1,"info":{}}`, want: "3.1"},
		{name: "a byte-order mark, block style", src: "\xef\xbb\xbfopenapi: 3.1.0\n", want: v},
		{name: "a byte-order mark, flow style", src: "\xef\xbb\xbf{\"openapi\":\"3.1.0\"}", want: v},
		{name: "an explicit document start", src: "---\nopenapi: 3.1.0\n", want: v},
		{name: "a comment before the document start", src: "# spec\n---\nopenapi: 3.1.0\n", want: v},
		// 1.1, because yaml.v3 refuses a 1.2 directive outright and the loader
		// shares that parser: such a document is undecodable at any size, so the
		// two readings cannot disagree on its format.
		{name: "a directive before the document start", src: "%YAML 1.1\n---\nopenapi: 3.1.0\n", want: v},
		{name: "a flow document after the marker", src: "--- {\"openapi\":\"3.1.0\"}\n", want: v},
		{name: "a flow document on the line after the marker", src: "---\n{\"openapi\":\"3.1.0\"}\n", want: v},
		{name: "a comment before a flow document", src: "# c\n\n  # d\n{\"openapi\":\"3.1.0\"}\n", want: v},
		// A root flow mapping is the whole document; a block entry written after
		// it is no root key, and the parse reads none.
		{name: "a block key after a flow document", src: "{\"openapi\":\"3.1.0\"}\nswagger: 2.0\n", want: v},
		{name: "an end marker after the key", src: "openapi: 3.1.0\n...\n", want: v},
		{name: "a plain scalar opening with dashes", src: "---- : 1\nopenapi: 3.1.0\n", want: v},
		{name: "a key opening with one dash", src: "-x: 1\nopenapi: 3.1.0\n", want: v},
		{name: "a key opening with one dot", src: ".x: 1\nopenapi: 3.1.0\n", want: v},
		// A root sequence is no mapping, so no key is found in it: the parse
		// refuses the root, and `- openapi` is not the name, so nothing claims
		// the bytes as this compiler's either.
		{name: "a root sequence", src: "- openapi: 3.1.0\n", reads: unreadable},
		// Bytes the compile never parses cannot name its format: load lowers the
		// first document that holds content, and none after it.
		{name: "the key in a second document", src: "kind: Foo\n---\nopenapi: 3.1.0\n"},
		{name: "the key after an end marker", src: "kind: Foo\n...\nopenapi: 3.1.0\n"},
		{name: "the key after a block scalar the marker ends", src: "text: |\n  line\n---\nopenapi: 3.1.0\n"},
		// And a leading document that holds nothing is not the document: the
		// parse steps past it to the one that does (GitHub #481). Every
		// spelling of nothing a person leaves at the top of a file is here — a
		// bare separator, a comment, each null token — and the key it reveals.
		{name: "the key after an empty first document", src: "---\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after two empty documents", src: "---\n---\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a comment-only document", src: "---\n# nothing\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a comment on the marker", src: "--- # nothing\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a null document", src: "--- null\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a tilde document", src: "--- ~\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an upper-case null document", src: "--- NULL\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a null on its own line", src: "---\nnull\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a null with a comment", src: "--- null # nothing\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a directive and an empty document", src: "%YAML 1.1\n---\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an empty document holding a directive", src: "---\n%YAML 1.1\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an empty document an end marker closes", src: "---\n...\n---\nopenapi: 3.1.0\n", want: v},
		// An anchor names a node and never decides what it resolves to, so an
		// anchored nothing is the nothing its unanchored spelling is.
		{name: "the key after an anchored empty document", src: "--- &a\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an anchored null", src: "--- &a null\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an anchor with only a comment beside it", src: "--- &a # c\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an anchor on its own line", src: "---\n&a\n---\nopenapi: 3.1.0\n", want: v},
		// A `&` naming nothing is no anchor, and yaml.v3 refuses the stream. The
		// key written at column 0 behind it is what makes the broken stream this
		// compiler's to report.
		{name: "the key after a nameless anchor", src: "--- &\n---\nopenapi: 3.1.0\n", reads: reported},
		// A quoted null is a string, and a string is content: the document with
		// it is the document, and its root is no mapping. The row is here so the
		// null tokens above are read as tokens; the key at column 0 after it is
		// what gets the refusal reported.
		{name: "the key after a quoted null document", src: "--- \"null\"\n---\nopenapi: 3.1.0\n", reads: reported},
		// `openapi:3.1.0` is a plain scalar, not a key: the parse refuses the
		// document for having a string at its root. The key guard does not ask
		// for the separating space, so the broken document is reported as this
		// compiler's (see declaresBlockKey).
		{name: "no space after the colon", src: "openapi:3.1.0\n", reads: reported},
		{name: "the name as a value", src: `{"note":"openapi"}`},
		// Column 0 is a root key only outside a flow collection or a quoted
		// scalar still open from a line above: yaml.v3 continues both there,
		// wherever the opener sat. A reader that took every column-0 line for a
		// root key claimed documents the parse reads as nesting the key.
		{name: "the key inside an open flow mapping", src: "info: {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside an open flow sequence", src: "info: [\nopenapi: 3.1.0\n]\n"},
		{name: "the key inside a flow mapping opened on an indented line", src: "info:\n  x: {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a flow mapping after a comment", src: "info: {a: \"x\", # c\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside an open double-quoted scalar", src: "description: \"a\nopenapi: 3.1.0\n\"\n"},
		{name: "the key inside an open single-quoted scalar", src: "description: 'a\nopenapi: 3.1.0\n'\n"},
		{name: "the key inside a single-quoted scalar with an escaped quote", src: "description: 'it''s\nopenapi: 3.1.0\n'\n"},
		{name: "the key inside a quoted scalar opened on an indented line", src: "info:\n  d: \"a\nopenapi: 3.1.0\n\"\n"},
		// A node property — an anchor, a tag, or both — stands between the colon
		// and the opener, and yaml.v3 opens the construct behind it all the same.
		{name: "the key inside a flow mapping behind an anchor", src: "a: &x {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a flow mapping behind a tag", src: "a: !!map {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a flow mapping behind a tag and an anchor", src: "a: !!map &x {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a quoted scalar behind an anchor", src: "a: &x \"s\nopenapi: 3.1.0\n\"\n"},
		{name: "the key inside a flow mapping item behind an anchor", src: "x:\n  - &a {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a quoted scalar behind an anchor in flow style", src: "a: {b: &x \"}\nopenapi: 3.1.0\n\"}\n"},
		{name: "a block key after an anchored flow document", src: "&x {\"a\":1}\nopenapi: 3.1.0\n"},
		{name: "an anchored flow document", src: "&x {\"openapi\":\"3.1.0\"}", want: v},
		{name: "a tagged flow document", src: "!!map {\"openapi\":\"3.1.0\"}", want: v},
		// And the shapes that look like an opener and are not, so the guard
		// above declines nothing the parse reads.
		{name: "the key after a flow mapping closed on its line", src: "info: {title: T}\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a flow mapping nesting collections", src: "info: {a: [1, {b: 2}]}\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a plain scalar with an unseparated colon", src: "url: http://x\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a flow mapping quoting a closer", src: "info: {title: \"}\"}\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a flow mapping with an apostrophe in a plain scalar", src: "info: {t: it's}\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a flow mapping commenting a closer", src: "info: {t: a # }\n}\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a plain scalar with an opener in it", src: "description: a [b\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a block scalar with an opener in it", src: "description: |\n  {\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a folded block scalar with a quote in it", src: "info:\n  d: >-\n    \"a\n\n    b\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a block scalar item with an opener in it", src: "x:\n  - |\n    [\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a block scalar with an indentation indicator", src: "d: |2\n   \"\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a block scalar behind an anchor", src: "a: &x |\n  {\nopenapi: 3.1.0\nb: x}\n", want: v},
		// The brace the scalar carries is closed on a later root line, so this
		// row reads the key only if the indicator was seen: a scalar whose
		// opener never closes reads it at the end of the document regardless.
		{name: "the key after a block scalar whose opener a later line closes", src: "d: |\n  {\nopenapi: 3.1.0\nb: x}\n", want: v},
		{name: "the key inside a flow mapping continued by a comment line", src: "info: {\n# c\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a quoted scalar escaping its line break", src: "d: \"a\\\nopenapi: 3.1.0\n\"\n"},
		{name: "the key after a quoted scalar closed on its line", src: "description: \"a # b\"\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a quoted scalar with an escaped quote", src: "description: \"a \\\" b\"\nopenapi: 3.1.0\n", want: v},
		// The cases the byte scan's own tests read, on the reader that remains.
		{name: "flow style, space around the colon", src: `{"openapi" : "3.1.0"}`, want: v},
		{name: "a bare scalar major in flow style", src: `{"openapi":3}`, want: "3"},
		{name: "block style, double quoted", src: "openapi: \"3.1.0\"\n", want: v},
		{name: "block style, single quoted", src: "openapi: '3.1.0'\n", want: v},
		{name: "block style, trailing comment", src: "openapi: 3.1.0 # the version\n", want: v},
		{name: "block style, no trailing newline", src: "openapi: 3.1.0", want: v},
		{name: "flow style, the name without a colon", src: `{"openapi","3.1.0"}`},
		{name: "flow style, nested one level down", src: `{"a":{"openapi":"3.1.0"}}`},
		{name: "block style, indented under another key", src: "a:\n  openapi: 3.1.0\n"},
		// A collection where the version goes is this compiler's document with
		// the wrong shape in it, so it is reported rather than declined.
		{name: "flow style, a collection for the version", src: `{"openapi":{"a":"3.1.0"}}`, reads: reported},
		{name: "flow style, broken before the key", src: `{"a":1,"b" 2,"openapi":"3.1.0"}`, reads: reported},
		{name: "flow style, ending at the colon", src: `{"openapi":`, reads: reported},
		{name: "flow style, a root sequence", src: `[{"openapi":"3.1.0"}]`, reads: unreadable},
		// Broken flow documents whose root mapping sits behind what YAML allows
		// in front of it, a comment line or a node property: the key is still
		// the root mapping's own, so the broken document is still reported.
		{name: "flow style, broken behind a comment line", src: "# c\n{\"openapi\":\"3.1.0\",", reads: reported},
		{name: "flow style, broken behind an anchor", src: "&x {\"openapi\":\"3.1.0\",", reads: reported},
		// Shapes a byte scan once had to decline, each for a reason of its own —
		// resolving an anchor or a merge, reading a tag, a key quoted as flow
		// style quotes it, a value continued onto the next line. The parse reads
		// them all, as it reads every row here.
		{name: "a root merge key", src: "base: &b\n  openapi: 3.1.0\n<<: *b\n", want: v},
		{name: "a quoted key in block style", src: "\"openapi\": 3.1.0\n", want: v},
		{name: "an unquoted key in flow style", src: "{openapi: 3.1.0}", want: v},
		{name: "an anchor before the version", src: "openapi: &v 3.1.0\n", want: v},
		{name: "a tag before the version", src: "openapi: !!str 3.1.0\n", want: v},
		{name: "a tagged null document before the spec", src: "--- !!null\n---\nopenapi: 3.1.0\n", want: v},
		{name: "a tagged null with a value before the spec", src: "--- !!null \"x\"\n---\nopenapi: 3.1.0\n", want: v},
		{name: "an anchored tagged null before the spec", src: "--- &a !!null\n---\nopenapi: 3.1.0\n", want: v},
		{name: "a tagged null carrying an anchor before the spec", src: "--- !!null &a\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the version on the line after the key", src: "openapi:\n  3.1.0\n", want: v},
		// A plain scalar may run on across several lines, and the parse reads all
		// of them: `3.1.0 more` is prose to the version guard, so nothing is
		// declared. A reader that took the first continuation line alone would
		// answer `3.1.0`, claiming the document by its first word.
		{name: "the version running on past the line after the key", src: "openapi:\n  3.1.0\n  more\n"},
	}
}

func TestDetect_ReadsWhatADocumentDeclares(t *testing.T) {
	t.Parallel()
	for _, tc := range declarations() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, _, err := sniff([]byte(tc.src))
			assert.Equal(t, sniffProbe{OpenAPI: tc.want}, probe)
			if tc.reads == parses {
				require.NoError(t, err, "the row reads as a document that parses")
			} else {
				require.Error(t, err, "the row reads as a document that does not parse")
			}

			rec, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(tc.src)}, compilers.Options{})
			assert.Equal(t, tc.want != "", ok, "a declared version is what recognizes the source")
			assert.Equal(t, majorMinor(tc.want), rec.Format.Version)
			var wantCodes []string
			if tc.reads == reported {
				wantCodes = []string{diag.UndecodableSource}
			}
			assert.Equal(t, wantCodes, codesOf(diags))
		})
	}
}

// TestDetect_BothKeysNameOneFormat pins a document that declares both keys. It
// names one format, OpenAPI, whichever key comes first and at any size: a
// reader that stopped at the first key it met read the same bytes as Swagger
// once they grew past the point where it took over.
func TestDetect_BothKeysNameOneFormat(t *testing.T) {
	t.Parallel()
	want := compilers.SourceFormat{Name: "openapi", Version: "3.0"}
	for name, src := range map[string]string{
		"small": `{"swagger":"2.0","openapi":"3.0.3"}`,
		"large": `{"swagger":"2.0","pad":"` + flowPad() + `","openapi":"3.0.3"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, _, ok := New().Detect(compilers.Source{Path: "spec.json", Data: []byte(src)}, compilers.Options{})
			assert.True(t, ok)
			assert.Equal(t, want, got.Format)
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
		{"a number and then prose", "openapi: 3 things to know\n"},
		{"flow style, prose for a version", `{"openapi":"is a format"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "README.md", Data: []byte(tc.src)}, compilers.Options{})
			assert.False(t, ok, "prose beside the word is not a declaration of this format")
			assert.Equal(t, compilers.SourceFormat{}, got.Format)
			assert.Nil(t, codesOf(diags), "another format's file earns no complaint from this one")
		})
	}
}

// TestDetect_AVersionThatIsNotServedIsStillADeclaration pins the other edge of
// the prose guard. A prerelease or otherwise unserved version is still one
// word beside the key, which is what a declaration looks like and prose does
// not; declining it would hand a document whose first line is `openapi:` to
// the engine's generic "unrecognized format", where claiming it lets load and
// the validator say exactly which version was wrong.
func TestDetect_AVersionThatIsNotServedIsStillADeclaration(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"a prerelease suffix", "openapi: 3.1.0-rc1\n", "3.1"},
		{"a build suffix", "openapi: 3.1.0+build.7\n", "3.1"},
		{"digits and then a word", "openapi: 3x\n", "3x"},
		{"a prerelease suffix in flow style", `{"openapi":"3.1.0-rc1"}`, "3.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(tc.src)}, compilers.Options{})
			assert.True(t, ok, "one word beside the key declares the format, whether or not it is served")
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: tc.want}, got.Format)
			assert.Nil(t, codesOf(diags), "which version is wrong is load's to say")
		})
	}
}

// TestDetect_ABrokenDocumentIsReportedAtAnySize pins what reading one way
// bought. A broken spec that declares the key at column 0 is this compiler's
// own, and saying so needs the whole document read: the byte scan that stood
// in for the parse past 64 KiB could not tell such a spec from another format's
// file naming the word, so the same document drew a complaint when small and
// silence when large.
func TestDetect_ABrokenDocumentIsReportedAtAnySize(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"small": "openapi: [unterminated\n",
		"large": padTo("openapi: [unterminated\n", "filler: x\n"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(src)}, compilers.Options{})
			assert.False(t, ok)
			assert.Equal(t, compilers.SourceFormat{}, got.Format)
			assert.Equal(t, []string{diag.UndecodableSource}, codesOf(diags))
		})
	}
}

// TestCompile_AnUnreadableSourceIsADiagnostic pins where the complaint about a
// broken document comes from when nothing detected it: a caller compiling
// directly hands Compile bytes no parse has read. It has to stay a diagnostic:
// engine.Run turns a compiler's Go error into its own, and the CLI maps that to
// exit 2 — the code it uses for being invoked wrong — so a spec it read would
// be reported as a misuse of itself.
func TestCompile_AnUnreadableSourceIsADiagnostic(t *testing.T) {
	t.Parallel()
	src := "bad: [unterminated\n" + strings.Repeat("filler: x\n", 8000) + "openapi: 3.1.0\n"
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{{Path: "api.yaml", Data: []byte(src)}}, compilers.Options{})

	require.NoError(t, err, "a document that will not parse is a finding, not a failure of the compiler")
	assert.Nil(t, doc)
	assert.Equal(t, []string{diag.UndecodableSource}, codesOf(diags))
}

// TestDetect_AnEmptyFirstDocumentIsNotTheDocument pins GitHub #481 at the
// public face: a stream that opens with a document holding nothing is routed on
// the document behind it, where it used to be declined as undecodable — with a
// whole spec two lines further down.
func TestDetect_AnEmptyFirstDocumentIsNotTheDocument(t *testing.T) {
	t.Parallel()
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}
	for name, lead := range map[string]string{
		"a bare separator":   "---\n---\n",
		"a null document":    "--- null\n---\n",
		"a comment document": "---\n# nothing\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := lead + "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
			got, diags, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte(src)}, compilers.Options{})
			assert.True(t, ok, "the document behind the empty one is what the source declares")
			assert.Equal(t, want, got.Format)
			assert.Nil(t, codesOf(diags))
		})
	}
}

// TestDetect_AByteOrderMarkIsNotAFormat pins a mark before the first byte,
// which the parse reads straight through and the key guard, reading bytes, has
// to skip itself: a broken spec written by an editor that emits one is as much
// this compiler's to report as the same spec without it.
func TestDetect_AByteOrderMarkIsNotAFormat(t *testing.T) {
	t.Parallel()
	const bom = "\xef\xbb\xbf"
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}
	for name, body := range map[string]string{
		"block style": "openapi: 3.1.0\n#" + flowPad() + "\n",
		"flow style":  `{"openapi":"3.1.0","pad":"` + flowPad() + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte(bom + body)}, compilers.Options{})
			assert.True(t, ok, "a byte-order mark is not part of what a document declares")
			assert.Equal(t, want, got.Format)
			assert.Nil(t, codesOf(diags))
		})
	}
	for name, body := range map[string]string{
		"broken, block style": "openapi: [unterminated\n",
		"broken, flow style":  `{"openapi":"3.1.0",`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte(bom + body)}, compilers.Options{})
			assert.False(t, ok)
			assert.Equal(t, []string{diag.UndecodableSource}, codesOf(diags), "the mark hides no key")
		})
	}
}

// TestDetect_CarriesTheParseItRead pins what a recognition hands over at any
// size: the parse that read the keys, which is what the compile lowers. Past
// 64 KiB detection used to scan and carry nothing, so a large document was
// parsed twice — once here and again in the compile.
func TestDetect_CarriesTheParseItRead(t *testing.T) {
	t.Parallel()
	const spec = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	for name, src := range map[string]string{
		"small": spec,
		"large": spec + "#" + flowPad() + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec, _, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte(src)}, compilers.Options{})
			require.True(t, ok)
			_, isParse := rec.Parsed.(*load.Parsed)
			assert.True(t, isParse, "the parse that read the keys is the one the compile lowers")
		})
	}
}
