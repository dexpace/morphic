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

// declarationRow is one document and the version it declares, or "" for one
// that declares none.
type declarationRow struct {
	name, src string
	want      string
}

// declarations are the shapes detection is held to. They were once a table
// holding two readers to one answer — a parse below a size cap, a byte scan
// above it — and every row that read differently either way carried a written
// reason. The scan is gone and the parse answers for every size, so each shape
// has one answer and the reasons went with the reader that needed them.
//
// The corpus is kept whole. Most of these rows exist because some reading once
// got them wrong: document markers in every position, flow collections and
// quoted scalars left open across lines, node properties in front of the value,
// block scalars carrying what looks like structure. That a single parser reads
// them correctly is not a reason to stop asking.
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
		{name: "a byte-order mark, block style", src: "\xef\xbb\xbfopenapi: 3.1.0\n", want: v},
		{name: "a byte-order mark, flow style", src: "\xef\xbb\xbf{\"openapi\":\"3.1.0\"}", want: v},
		{name: "an explicit document start", src: "---\nopenapi: 3.1.0\n", want: v},
		{name: "a comment before the document start", src: "# spec\n---\nopenapi: 3.1.0\n", want: v},
		{name: "a directive before the document start", src: "%YAML 1.1\n---\nopenapi: 3.1.0\n", want: v},
		{name: "a flow document after the marker", src: "--- {\"openapi\":\"3.1.0\"}\n", want: v},
		{name: "a flow document on the line after the marker", src: "---\n{\"openapi\":\"3.1.0\"}\n", want: v},
		{name: "a comment before a flow document", src: "# c\n\n  # d\n{\"openapi\":\"3.1.0\"}\n", want: v},
		{name: "a block key after a flow document", src: "{\"openapi\":\"3.1.0\"}\nswagger: 2.0\n", want: v},
		{name: "an end marker after the key", src: "openapi: 3.1.0\n...\n", want: v},
		{name: "a plain scalar opening with dashes", src: "---- : 1\nopenapi: 3.1.0\n", want: v},
		{name: "a key opening with one dash", src: "-x: 1\nopenapi: 3.1.0\n", want: v},
		{name: "a key opening with one dot", src: ".x: 1\nopenapi: 3.1.0\n", want: v},
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
		{name: "the key after an anchored empty document", src: "--- &a\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an anchored null", src: "--- &a null\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an anchor with only a comment beside it", src: "--- &a # c\n---\nopenapi: 3.1.0\n", want: v},
		{name: "the key after an anchor on its own line", src: "---\n&a\n---\nopenapi: 3.1.0\n", want: v},
		{name: "an anchored flow document", src: "&x {\"openapi\":\"3.1.0\"}", want: v},
		{name: "a tagged flow document", src: "!!map {\"openapi\":\"3.1.0\"}", want: v},
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
		{name: "the key after a block scalar whose opener a later line closes", src: "d: |\n  {\nopenapi: 3.1.0\nb: x}\n", want: v},
		{name: "the key after a quoted scalar closed on its line", src: "description: \"a # b\"\nopenapi: 3.1.0\n", want: v},
		{name: "the key after a quoted scalar with an escaped quote", src: "description: \"a \\\" b\"\nopenapi: 3.1.0\n", want: v},
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
		{name: "a root sequence", src: "- openapi: 3.1.0\n"},
		{name: "the key in a second document", src: "kind: Foo\n---\nopenapi: 3.1.0\n"},
		{name: "the key after an end marker", src: "kind: Foo\n...\nopenapi: 3.1.0\n"},
		{name: "the key after a block scalar the marker ends", src: "text: |\n  line\n---\nopenapi: 3.1.0\n"},
		{name: "the key after a nameless anchor", src: "--- &\n---\nopenapi: 3.1.0\n"},
		{name: "the key after a quoted null document", src: "--- \"null\"\n---\nopenapi: 3.1.0\n"},
		{name: "no space after the colon", src: "openapi:3.1.0\n"},
		{name: "the name as a value", src: `{"note":"openapi"}`},
		{name: "the key inside an open flow mapping", src: "info: {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside an open flow sequence", src: "info: [\nopenapi: 3.1.0\n]\n"},
		{name: "the key inside a flow mapping opened on an indented line", src: "info:\n  x: {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a flow mapping after a comment", src: "info: {a: \"x\", # c\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside an open double-quoted scalar", src: "description: \"a\nopenapi: 3.1.0\n\"\n"},
		{name: "the key inside an open single-quoted scalar", src: "description: 'a\nopenapi: 3.1.0\n'\n"},
		{name: "the key inside a single-quoted scalar with an escaped quote", src: "description: 'it''s\nopenapi: 3.1.0\n'\n"},
		{name: "the key inside a quoted scalar opened on an indented line", src: "info:\n  d: \"a\nopenapi: 3.1.0\n\"\n"},
		{name: "the key inside a flow mapping behind an anchor", src: "a: &x {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a flow mapping behind a tag", src: "a: !!map {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a flow mapping behind a tag and an anchor", src: "a: !!map &x {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a quoted scalar behind an anchor", src: "a: &x \"s\nopenapi: 3.1.0\n\"\n"},
		{name: "the key inside a flow mapping item behind an anchor", src: "x:\n  - &a {\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a quoted scalar behind an anchor in flow style", src: "a: {b: &x \"}\nopenapi: 3.1.0\n\"}\n"},
		{name: "a block key after an anchored flow document", src: "&x {\"a\":1}\nopenapi: 3.1.0\n"},
		{name: "the key inside a flow mapping continued by a comment line", src: "info: {\n# c\nopenapi: 3.1.0\n}\n"},
		{name: "the key inside a quoted scalar escaping its line break", src: "d: \"a\\\nopenapi: 3.1.0\n\"\n"}}
}

// TestSniff_ReadsWhatADocumentDeclares holds detection's one reading to the
// corpus above: what a document says it is, read off the parsed document and
// nowhere else.
func TestSniff_ReadsWhatADocumentDeclares(t *testing.T) {
	t.Parallel()
	for _, tc := range declarations() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, _, _ := sniff([]byte(tc.src))
			assert.Equal(t, sniffProbe{OpenAPI: tc.want}, probe)
		})
	}
}

// TestDetect_DeclinesASourcePastItsCeiling pins the bound that replaced the
// sniff cap. Past it nothing is read: the document is not parsed, so no format
// is named and no parse is carried — which is cheaper than what came before,
// where such a source was scanned end to end and then refused by the byte
// budget anyway.
//
// The refusal is not silent when the source is recognizably this compiler's.
// "No compiler took it" is the wrong thing to say about a 100 MB OpenAPI
// document, and the key is read by a linear scan that costs nothing next to
// the parse it is declining to do.
func TestDetect_DeclinesASourcePastItsCeiling(t *testing.T) {
	t.Parallel()
	head := []byte("openapi: 3.1.0\n")

	keyed := make([]byte, maxDetectBytes+1)
	copy(keyed, head)
	rec, diags, ok := New().Detect(compilers.Source{Path: "huge.yaml", Data: keyed})
	assert.False(t, ok, "a source past the ceiling is not read, so it declares nothing here")
	assert.Equal(t, compilers.SourceFormat{}, rec.Format)
	assert.True(t, rec.Parsed == nil, "nothing was parsed, and a typed nil is not nothing")
	require.Equal(t, []string{diag.UndecodableSource}, codesOf(diags))
	assert.Contains(t, diags[0].Message, "ceiling detection reads", "the refusal says what it refused on")

	anonymous := make([]byte, maxDetectBytes+1)
	_, diags, ok = New().Detect(compilers.Source{Path: "huge.bin", Data: anonymous})
	assert.False(t, ok)
	assert.Nil(t, codesOf(diags), "bytes that name no key of ours are nobody's business here")

	// A document exactly at the ceiling, padded with a comment so the padding
	// is legal YAML rather than bytes the parser would refuse.
	prefix := string(head) + "info: {title: T, version: \"1\"}\npaths: {}\n#"
	atCeiling := []byte(prefix + strings.Repeat("p", maxDetectBytes-len(prefix)-1) + "\n")
	require.Len(t, atCeiling, maxDetectBytes, "the case must sit exactly on the ceiling")
	rec, diags, ok = New().Detect(compilers.Source{Path: "big.yaml", Data: atCeiling})
	require.True(t, ok, "the ceiling is a bound on what is read, not one byte below it: %v", codesOf(diags))
	assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, rec.Format)
	assert.NotNil(t, rec.Parsed, "and what it read is what the compile lowers")
}

// TestDetect_AnUnreadableSourceNamingTheKeyIsClaimed pins what changed for a
// document that will not parse: the complaint no longer depends on the
// document's size.
//
// A source that writes `openapi:` at the start of a line and then will not read
// is this compiler's own and broken, which nothing else is in a position to
// say. The scan could not tell that from another format's file, so above the
// cap such a source was declined in silence and below it was reported — the
// same bytes, two answers, decided by length. Now it is reported at every size.
func TestDetect_AnUnreadableSourceNamingTheKeyIsClaimed(t *testing.T) {
	t.Parallel()
	const prose = "openapi: is a format\n"
	for name, src := range map[string]string{
		"a short note":         "# Notes\n\ntext\n" + prose,
		"a long note":          "# Notes\n\n" + strings.Repeat("filler text\n", 8000) + prose,
		"an unterminated flow": "openapi: [unterminated\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags, ok := New().Detect(compilers.Source{Path: "README.md", Data: []byte(src)})
			assert.False(t, ok, "it declares no version this compiler can read")
			assert.Equal(t, []string{diag.UndecodableSource}, codesOf(diags))
		})
	}
}

// TestCompile_AnUnreadableSourceIsADiagnostic pins where the complaint about a
// broken document comes from. It has to stay a diagnostic: engine.Run turns a
// compiler's Go error into its own, and the CLI maps that to exit 2 — the code
// it uses for being invoked wrong — so a spec it read would be reported as a
// misuse of itself.
func TestCompile_AnUnreadableSourceIsADiagnostic(t *testing.T) {
	t.Parallel()
	src := "bad: [unterminated\n" + strings.Repeat("filler: x\n", 8000) + "openapi: 3.1.0\n"
	doc, diags, err := New().Compile(context.Background(),
		[]compilers.Source{{Path: "api.yaml", Data: []byte(src)}}, compilers.Options{})

	require.NoError(t, err, "a document that will not parse is a finding, not a failure of the compiler")
	assert.Nil(t, doc)
	assert.Equal(t, []string{diag.UndecodableSource}, codesOf(diags))
}

// TestDetect_CarriesTheParseItRead pins what a recognition hands over: the
// document that named the format is the document the compile lowers, so the
// source is read once rather than once per question asked of it.
func TestDetect_CarriesTheParseItRead(t *testing.T) {
	t.Parallel()
	rec, _, ok := New().Detect(compilers.Source{
		Path: "spec.yaml",
		Data: []byte("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"),
	})

	require.True(t, ok)
	assert.NotNil(t, rec.Parsed, "the parse that read the keys is the one the compile lowers")
}

// BenchmarkDetect_ParsesTheDocument measures what recognizing a source now
// costs, on a document large enough for the parse to dominate. It replaced a
// benchmark of the byte scan that answered the same question without building
// a tree — and without leaving one for the compile, which then built its own.
func BenchmarkDetect_ParsesTheDocument(b *testing.B) {
	var doc strings.Builder
	doc.WriteString("openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths:\n")
	for doc.Len() < 1<<20 {
		doc.WriteString("  /p" + strings.Repeat("x", 40) + ": {get: {operationId: op, responses: {\"200\": {description: ok}}}}\n")
	}
	src := compilers.Source{Path: "big.yaml", Data: []byte(doc.String())}

	b.SetBytes(int64(len(src.Data)))
	for b.Loop() {
		if _, _, ok := New().Detect(src); !ok {
			b.Fatal("the document declares a version this compiler serves")
		}
	}
}

// TestDetect_WhereTheVersionSitsDoesNotDecideTheFormat pins that a document is
// read whole. A mapping's keys are unordered, so the same spec written with its
// version key first and with it last is one document, and a reader that stopped
// early answered differently for the two. Nothing stops early now — the cap
// that made this a question is gone — and the document that declares both keys
// is here for the same reason: which key a reader reaches first is not a
// property of the document.
func TestDetect_WhereTheVersionSitsDoesNotDecideTheFormat(t *testing.T) {
	t.Parallel()
	var flow, block strings.Builder
	flow.WriteString(`"components":{"schemas":{`)
	block.WriteString("components:\n  schemas:\n")
	for i := range 200 {
		if i > 0 {
			flow.WriteByte(',')
		}
		fmt.Fprintf(&flow, `"S%d":{"type":"object","description":"a schema"}`, i)
		fmt.Fprintf(&block, "    S%d:\n      type: object\n      description: a schema\n", i)
	}
	flow.WriteString(`}}`)

	want := compilers.SourceFormat{Name: "openapi", Version: "3.0"}
	for name, src := range map[string]string{
		"flow json, version first":  `{"openapi":"3.0.3",` + flow.String() + `}`,
		"flow json, version last":   `{` + flow.String() + `,"openapi":"3.0.3"}`,
		"block yaml, version first": "openapi: 3.0.3\n" + block.String(),
		"block yaml, version last":  block.String() + "openapi: 3.0.3\n",
		"both keys, openapi last":   `{"swagger":"2.0","openapi":"3.0.3"}`,
		"both keys, openapi first":  `{"openapi":"3.0.3","swagger":"2.0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "spec.json", Data: []byte(src)})
			assert.True(t, ok, "a valid document must not be declined over where it declares its version")
			assert.Equal(t, want, got.Format)
			assert.Nil(t, codesOf(diags), "a document this compiler recognizes carries no complaint")
		})
	}
}

// TestDetect_AValueThatIsNoVersionIsNoDeclaration pins whose bytes these are. A
// key alone does not declare a format: prose sitting beside the word is what a
// document of another format writes, and claiming it reports this compiler's
// complaint over a file that was never its own.
func TestDetect_AValueThatIsNoVersionIsNoDeclaration(t *testing.T) {
	t.Parallel()
	for name, src := range map[string]string{
		"prose beside the key":            "openapi: is a format\n",
		"a pointer beside the key":        "openapi: see the docs\n",
		"prose beside swagger":            "swagger: yes\n",
		"a number and then prose":         "openapi: 3 things to know\n",
		"flow style, prose for a version": `{"openapi":"is a format"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "README.md", Data: []byte(src)})
			assert.False(t, ok, "prose beside the word is not a declaration of this format")
			assert.Equal(t, compilers.SourceFormat{}, got.Format)
			assert.Nil(t, codesOf(diags), "another format's file earns no complaint from this one")
		})
	}
}

// TestDetect_AVersionThatIsNotServedIsStillADeclaration pins the other edge of
// the prose guard. A prerelease or otherwise unserved version is still one word
// beside the key, which is what a declaration looks like and prose is not;
// declining it would hand a document whose first line is `openapi:` to the
// engine's generic "unrecognized format", where claiming it lets load and the
// validator say exactly which version was wrong.
func TestDetect_AVersionThatIsNotServedIsStillADeclaration(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ src, want string }{
		"a prerelease suffix":        {"openapi: 3.1.0-rc1\n", "3.1"},
		"a build suffix":             {"openapi: 3.1.0+build.7\n", "3.1"},
		"digits and then a word":     {"openapi: 3x\n", "3x"},
		"a prerelease in flow style": {`{"openapi":"3.1.0-rc1"}`, "3.1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(tc.src)})
			assert.True(t, ok, "one word beside the key declares the format, whether or not it is served")
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: tc.want}, got.Format)
			assert.Nil(t, codesOf(diags), "which version is wrong is load's to say")
		})
	}
}

// TestDetect_AnEmptyFirstDocumentIsNotTheDocument pins GitHub #481 at the
// public face: a stream that opens with a document holding nothing is routed on
// the document behind it. It used to be declined as undecodable, with a whole
// spec two lines further down.
func TestDetect_AnEmptyFirstDocumentIsNotTheDocument(t *testing.T) {
	t.Parallel()
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}
	for name, lead := range map[string]string{
		"a bare separator":   "---\n---\n",
		"a null document":    "--- null\n---\n",
		"a comment document": "---\n# nothing\n---\n",
		"an anchored null":   "--- &a null\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := lead + "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
			got, diags, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte(src)})
			assert.True(t, ok, "the document behind the empty one is what the source declares")
			assert.Equal(t, want, got.Format)
			assert.Nil(t, codesOf(diags))
		})
	}
}

// TestDetect_AByteOrderMarkIsNotAFormat pins that a mark before the first byte
// is not part of what a document declares — on a file no editor thinks is
// unusual. The parse reads through one; the guard that claims an unreadable
// source steps over it itself, which is what its own byte-order-mark rows pin.
func TestDetect_AByteOrderMarkIsNotAFormat(t *testing.T) {
	t.Parallel()
	want := compilers.SourceFormat{Name: "openapi", Version: "3.1"}
	for name, body := range map[string]string{
		"block style": "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n",
		"flow style":  `{"openapi":"3.1.0","info":{"title":"T","version":"1"},"paths":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "spec.yaml", Data: []byte("\xef\xbb\xbf" + body)})
			assert.True(t, ok, "a byte-order mark is not part of what a document declares")
			assert.Equal(t, want, got.Format)
			assert.Nil(t, codesOf(diags))
		})
	}
}
