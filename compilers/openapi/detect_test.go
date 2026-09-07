package openapi

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

func TestDetect_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, path, src string
		want            compilers.SourceFormat
		wantOK          bool
		// wantCode is the codes of the diagnostics a decline carries. Empty for
		// every source this compiler recognizes, and for every source that is
		// another format's business.
		wantCode []string
	}{
		{"openapi 3.1 yaml", "api.yaml", "openapi: 3.1.0\ninfo: {}\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true, nil},
		{"openapi 3.0 json", "api.json", `{"openapi": "3.0.3"}`,
			compilers.SourceFormat{Name: "openapi", Version: "3.0"}, true, nil},
		{"openapi 3.2 yaml", "api.yaml", "openapi: 3.2.0\ninfo: {}\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.2"}, true, nil},
		// A version already in major.minor form (single dot) exercises
		// majorMinor's unchanged-passthrough return.
		{"openapi major.minor only", "api.yaml", "openapi: \"3.1\"\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true, nil},
		// A bare-major version (no dot) also reaches the passthrough return.
		{"openapi bare major", "api.yaml", "openapi: \"4\"\n",
			compilers.SourceFormat{Name: "openapi", Version: "4"}, true, nil},
		// Recognized, and deliberately not a format Formats reports: naming it
		// lets the caller say "unsupported" rather than "unreadable".
		{"swagger", "api.yaml", "swagger: \"2.0\"\n",
			compilers.SourceFormat{Name: "swagger", Version: "2.0"}, true, nil},

		// Everything below is another format's input, and none of it is an error
		// here: three of the five planned compilers take bytes that are not YAML.
		{"protobuf", "svc.proto", "syntax = \"proto3\";\nservice S { rpc Get (Q) returns (A); }\n",
			compilers.SourceFormat{}, false, nil},
		{"typespec", "main.tsp", "import \"@typespec/http\";\nmodel Pet { name: string; }\n",
			compilers.SourceFormat{}, false, nil},
		{"graphql", "s.graphql", "type Query {\n  pet(id: ID!): Pet\n}\n",
			compilers.SourceFormat{}, false, nil},
		// Valid YAML that declares neither key: whether a source parses is not
		// the question detection answers.
		{"graphql that parses as yaml", "s.graphql", "type Query { a: String }\n",
			compilers.SourceFormat{}, false, nil},
		{"yaml that is no spec", "junk.yaml", "hello: world\n",
			compilers.SourceFormat{}, false, nil},
		// Declares an openapi key and will not parse: this compiler's own source,
		// broken, which nothing else is in a position to say.
		{"unparseable yaml", "api.yaml", "openapi: [unterminated\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		{"unparseable json", "api.json", `{"openapi": "3.1.0", "info": {`,
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// Broken, and never this compiler's: the key it names is a value, not a
		// key, so the parse error describes a parser that was wrong to be asked.
		{"unparseable, key only mentioned", "svc.proto", "syntax = \"openapi\";\n{[",
			compilers.SourceFormat{}, false, nil},
		// Past the sniff cap and still this compiler's: the key it declares is in
		// the prefix, so the fast path alone is enough to call it broken rather
		// than somebody else's.
		{"unparseable past the cap", "api.yaml",
			padTo("openapi: [unterminated\n", "filler: x\n"),
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// Declares the key only past the cap, on a prefix that does not parse. The
		// key search reads every byte, so the declaration is found and the source
		// is this compiler's own — broken, and said so, rather than declined as
		// somebody else's for want of looking.
		{"key past the cap on an unparseable prefix", "api.yaml",
			padTo("bad: [unterminated\n", "filler: x\n") + "openapi: 3.1.0\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// The same case in flow style, which is what the motivating spec is
		// written in. A JSON document has no line structure to cut at, so its
		// prefix is streamed and the stream stops at the cut; the whole read is
		// what finds the declaration, and keeping that read's own error is what
		// lets the answer be "this compiler's, and broken" rather than silence.
		{"key past the cap on an unparseable flow prefix", "spec3.json",
			`{"pad":"` + flowPad() + `","bad" 1,"openapi":"3.1.0"}`,
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// Another format's document, past the cap, naming the word as a key and
		// broken besides. It opens no mapping of its own, so the key is not its
		// declaration of itself and this compiler has nothing to say: reporting a
		// parse error here would claim bytes that were never its own.
		{"a broken document of another format names the key", "asyncapi.json",
			`[{"openapi":"3.1.0"},"` + flowPad() + `"`,
			compilers.SourceFormat{}, false, nil},
		// The same, one level down inside a mapping that does open the document.
		// A nested key names a field, not the format of the file holding it.
		{"a broken document of another format nests the key", "other.json",
			`{"pad":"` + flowPad() + `","deep":{"openapi":"3.1.0"},"bad" 1}`,
			compilers.SourceFormat{}, false, nil},
		{"empty", "empty.yaml", "", compilers.SourceFormat{}, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: tc.path, Data: []byte(tc.src)})
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantCode, codesOf(diags),
				"a decline says something only when the source is recognizably this compiler's")
		})
	}
}

// TestDetect_KeyOrderDoesNotDecideTheFormat is the shape this bound was getting
// wrong: a published spec whose `components` object runs to megabytes and whose
// `openapi` key sits behind it. A JSON object's keys are unordered, so the same
// document written with its version key first and with it last is one document,
// and detection has to answer the same for both. Only the version-last spellings
// go past the prefix — they are the cases a prefix-only sniff declines.
func TestDetect_KeyOrderDoesNotDecideTheFormat(t *testing.T) {
	t.Parallel()
	flow, block := bigComponents()
	cases := []struct{ name, path, src string }{
		{"flow json, version first", "spec3.json", `{"openapi":"3.0.3",` + flow + `}`},
		{"flow json, version last", "spec3.json", `{` + flow + `,"openapi":"3.0.3"}`},
		{"block yaml, version first", "api.yaml", "openapi: 3.0.3\n" + block},
		{"block yaml, version last", "api.yaml", block + "openapi: 3.0.3\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(tc.src), maxSniffBytes, "the case must exceed the cap to test it")
			got, diags, ok := New().Detect(compilers.Source{Path: tc.path, Data: []byte(tc.src)})
			assert.True(t, ok, "a valid document must not be declined over where it declares its version")
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, got)
			assert.Nil(t, codesOf(diags), "a document this compiler recognizes carries no complaint")
		})
	}
}

// bigComponents returns a `components` entry whose value alone runs past the
// sniff cap, in flow and in block style. It stands in for the schema catalogue a
// published spec leads with; what matters is only that it is one entry too large
// to read past.
func bigComponents() (flow, block string) {
	var f, b strings.Builder
	f.WriteString(`"components":{"schemas":{`)
	b.WriteString("components:\n  schemas:\n")
	for i := 0; f.Len() <= maxSniffBytes || b.Len() <= maxSniffBytes; i++ {
		if i > 0 {
			f.WriteByte(',')
		}
		fmt.Fprintf(&f, `"S%d":{"type":"object","description":"a schema"}`, i)
		fmt.Fprintf(&b, "    S%d:\n      type: object\n      description: a schema\n", i)
	}
	f.WriteString(`}}`)
	return f.String(), b.String()
}

// flowPad returns a run of bytes long enough that a flow entry holding it puts
// everything after it past the sniff cap.
func flowPad() string { return strings.Repeat("p", maxSniffBytes) }

// padTo returns src grown past the sniff cap by appending filler, so sniff reads
// a prefix first rather than decoding the source whole on sight.
func padTo(src, filler string) string {
	var b strings.Builder
	b.WriteString(src)
	for b.Len() <= maxSniffBytes {
		b.WriteString(filler)
	}
	return b.String()
}

// TestSniff_BeyondTheCap pins both paths a document larger than the cap can
// take. The prefix answers on its own whenever it names a key, in whichever
// style the document is written; when it names neither, a document whose bytes
// name one further in is read whole rather than declined, because where a writer
// put a key in a mapping says nothing about what the document is. Bytes that
// name neither key anywhere never leave the prefix.
func TestSniff_BeyondTheCap(t *testing.T) {
	t.Parallel()
	const filler = "# a line of padding that says nothing about the format\n"
	pad := strings.Repeat("p", maxSniffBytes)
	cases := []struct {
		name, src string
		want      sniffProbe
	}{
		{"block yaml declaring first",
			padTo("openapi: 3.1.0\n", filler), sniffProbe{OpenAPI: "3.1.0"}},
		{"block yaml declaring past the cap",
			padTo("", filler) + "openapi: 3.1.0\n", sniffProbe{OpenAPI: "3.1.0"}},
		{"flow json declaring first",
			`{"openapi":"3.1.0","x":"` + pad + `"}`, sniffProbe{OpenAPI: "3.1.0"}},
		{"flow json declaring past the cap",
			`{"x":"` + pad + `","openapi":"3.1.0"}`, sniffProbe{OpenAPI: "3.1.0"}},
		{"flow json swagger first",
			`{"swagger":"2.0","x":"` + pad + `"}`, sniffProbe{Swagger: "2.0"}},
		{"flow json swagger past the cap",
			`{"x":"` + pad + `","swagger":"2.0"}`, sniffProbe{Swagger: "2.0"}},
		// Neither YAML nor JSON, and larger than the cap: the prefix is parsed,
		// fails, and the answer is silence rather than a parser's complaint.
		{"protobuf past the cap",
			padTo("syntax = \"proto3\";\n", "message M { string a = 1; }\n"), sniffProbe{}},
		// The word is there past the cap and is not a key, so the whole read is
		// never reached — asserted on the guard itself below, since the probe a
		// whole read would return here is the zero one either way.
		{"the word past the cap is not a key",
			`{"x":"` + pad + `","note":"openapi"}`, sniffProbe{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(tc.src), maxSniffBytes, "the case must exceed the cap to test it")
			probe, _ := sniff([]byte(tc.src))
			assert.Equal(t, tc.want, probe,
				"the error is not asserted: a prefix of another format is unreadable here by design")
		})
	}
}

// TestDeclaresProbeKey_GuardsTheWholeRead pins the one decision that keeps a
// document of another format off the slow path: the whole of a source is scanned
// for a key, and only a declaration — the name with the colon that makes it one
// — counts as having found it.
func TestDeclaresProbeKey_GuardsTheWholeRead(t *testing.T) {
	t.Parallel()
	pad := strings.Repeat("p", maxSniffBytes)
	cases := []struct {
		name, src string
		want      bool
	}{
		{"declared past the cap in flow style", `{"x":"` + pad + `","openapi":"3.1.0"}`, true},
		{"declared past the cap in block style", "x: " + pad + "\nswagger: \"2.0\"\n", true},
		{"named past the cap as a value", `{"x":"` + pad + `","note":"openapi"}`, false},
		{"named past the cap in prose", "x: " + pad + "\n# openapi is a format\n", false},
		// A key, and still not this document's: it names a field of something
		// nested, which says nothing about the format of the file around it.
		{"named past the cap as a nested key", `{"x":"` + pad + `","in":{"openapi":"3.1.0"}}`, false},
		// A document that opens no mapping declares no top-level key at all, so
		// whatever its members name, none of it is a declaration of this format.
		{"named past the cap in a document that opens no mapping",
			`[{"x":"` + pad + `"},{"openapi":"3.1.0"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(tc.src), maxSniffBytes, "the case must exceed the cap to test it")
			assert.Equal(t, tc.want, declaresProbeKey([]byte(tc.src)))
		})
	}
}

// TestDecodeFlowEntries_ReadsWhatTheCutLeft pins both halves of what the walk
// returns: the entries it completed, and whether the token stream broke before
// the mapping closed. A cut prefix breaks it by construction and a whole
// document does not, which is why the error is reported rather than folded into
// the probe — the same bytes mean "cut here" to one caller and "unreadable" to
// the other, and only the caller knows which it passed.
// TestDeclaresProbeKey_ScopesTheNameToItsOwnDocument pins the half of the guard
// that decides whose bytes these are. A name followed by a colon is a key
// wherever it sits, so the scan has to say *whose* key: block style answers with
// column 0, flow style with the root mapping's own depth. Everything below is a
// document naming the word somewhere it does not declare this format, and the
// answer for each is no — a compiler that says otherwise reports its own parse
// error over a file that was never its own.
func TestDeclaresProbeKey_ScopesTheNameToItsOwnDocument(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      bool
	}{
		{"flow mapping declares it", `{"openapi":"3.1.0"}`, true},
		{"flow mapping declares swagger", `{"swagger":"2.0"}`, true},
		{"space around the mapping and the colon", "  \n\t{\"openapi\" : \"3.1.0\"}", true},
		{"an escape hides no key from the scan", `{"a\"b":1,"openapi":"3.1.0"}`, true},
		{"block style at column 0", "openapi: 3.1.0\n", true},

		{"nested one level down", `{"a":{"openapi":"3.1.0"}}`, false},
		{"nested inside a sequence", `{"a":[{"openapi":"3.1.0"}]}`, false},
		{"a document that opens a sequence", `[{"openapi":"3.1.0"}]`, false},
		{"block style indented under another key", "a:\n  openapi: 3.1.0\n", false},
		{"the name is a value", `{"note":"openapi"}`, false},
		{"the name has no colon after it", `{"openapi",1}`, false},
		{"the name ends the bytes", `{"openapi"`, false},
		{"a string runs off the end", `{"a":"unterminated`, false},
		{"nothing but whitespace", "  \n\t ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, declaresProbeKey([]byte(tc.src)))
		})
	}
}

func TestDecodeFlowEntries_ReadsWhatTheCutLeft(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, prefix       string
		want               sniffProbe
		wantFlow, wantBrok bool
	}{
		{"complete document", `{"openapi":"3.1.0","info":{"title":"T"}}`,
			sniffProbe{OpenAPI: "3.1.0"}, true, false},
		{"cut inside a later value", `{"openapi":"3.1.0","info":{"title":"T`,
			sniffProbe{OpenAPI: "3.1.0"}, true, true},
		{"cut inside a key", `{"openapi":"3.1.0","inf`,
			sniffProbe{OpenAPI: "3.1.0"}, true, true},
		{"swagger", `{"swagger":"2.0","info":{}}`, sniffProbe{Swagger: "2.0"}, true, false},
		// A version that is not a string declares no dialect, and must not be
		// read as one by accident.
		{"non-string version", `{"openapi":3}`, sniffProbe{}, true, false},
		// Whole, opens a mapping, and breaks in the middle of it: nothing was cut
		// away, so the break is the document's own.
		{"malformed mid-mapping", `{"a":1,"b" 2,"openapi":"3.1.0"}`,
			sniffProbe{}, true, true},
		{"no flow mapping", "openapi: 3.1.0\n", sniffProbe{}, false, false},
		{"not even a token", "\x00", sniffProbe{}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, flow, err := decodeFlowEntries([]byte(tc.prefix))
			assert.Equal(t, tc.wantFlow, flow)
			assert.Equal(t, tc.want, got)
			if tc.wantBrok {
				assert.Error(t, err, "a broken token stream is reported, not swallowed")
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestDecodeFlowEntries_StopsAtTheEntryCap proves the walk is bounded by its own
// count and not only by the byte cap: a declaration after maxSniffEntries other
// entries is not read.
func TestDecodeFlowEntries_StopsAtTheEntryCap(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteByte('{')
	for i := range maxSniffEntries + 1 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"k`)
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(strings.Repeat("y", i%7))
		b.WriteString(`":0`)
	}
	b.WriteString(`,"openapi":"3.1.0"}`)

	got, flow, err := decodeFlowEntries([]byte(b.String()))
	require.True(t, flow)
	require.NoError(t, err, "stopping on the cap is not the document breaking")
	assert.Equal(t, sniffProbe{}, got, "the entry past the cap is not read")
}

func TestWholeLines(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a\nb\n", string(wholeLines([]byte("a\nb\nc"))))
	assert.Equal(t, "nolines", string(wholeLines([]byte("nolines"))),
		"a prefix with no newline has no better cut to make")
}

func TestMajorMinor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "3.1", majorMinor("3.1.0"))
	assert.Equal(t, "3.1", majorMinor("3.1"))
	assert.Equal(t, "4", majorMinor("4"))
}

// codesOf reduces diagnostics to their codes, which is the whole of what the
// detection tests assert about them: the message carries a parser's wording and
// pinning it would test yaml.v3 rather than this package.
func codesOf(diags []ir.Diagnostic) []string {
	if len(diags) == 0 {
		return nil
	}
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return codes
}
