package openapi

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

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
		// Past the cap, where detection scans rather than parses, and the key it
		// writes has no version beside it. A scan cannot tell that from another
		// format's file naming the word, and claiming the wrong one of those two
		// is the costlier mistake, so it declines and the caller is told the
		// format was not recognized.
		{"unparseable past the cap", "api.yaml",
			padTo("openapi: [unterminated\n", "filler: x\n"),
			compilers.SourceFormat{}, false, nil},
		// Declares the key only past the cap, on a prefix that does not parse. The
		// scan reads every byte, so the version is found and the format named; that
		// the bytes around it will not parse is the compile's finding to report,
		// where the parse that discovers it is one the loader had agreed to pay
		// for. See TestCompile_AnUnreadableSourceIsADiagnostic.
		{"key past the cap on an unparseable prefix", "api.yaml",
			padTo("bad: [unterminated\n", "filler: x\n") + "openapi: 3.1.0\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true, nil},
		// The same case in flow style, which is what the motivating spec is written
		// in. A JSON document has no line structure to cut at, and the scan needs
		// none: it tracks nesting through bytes a parser stops at.
		{"key past the cap on an unparseable flow prefix", "spec3.json",
			`{"pad":"` + flowPad() + `","bad" 1,"openapi":"3.1.0"}`,
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true, nil},
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

// dupRepeats is how many times the fixtures below repeat a root key. It is not a
// threshold: the wrong answer reproduces at two repeats, and this many only
// makes the fixture recognizably a document rather than a corner. It is
// deliberately far below the 6,553 of the report — yaml.v3 raises one error per
// pair of matching keys, so that count produced 21,467,628 of them and a 1.2 GB
// message, and a fixture that large turns a revert into an out-of-memory kill
// instead of a failing assertion. What guards the cost is
// TestSniff_CostIsNotQuadraticInRepeatedKeys, which measures growth rather than
// paying for it.
const dupRepeats = 512

// TestDetect_RepeatedKeysDoNotDecideTheFormat pins the fix for the blow-up. A
// document that repeats a top-level key is a document with a duplicate key —
// the parser this compiler goes on to use says so, once per repeat and sited —
// and it is not a document of another format, nor one that cannot be read.
// Detection used to answer both of those, because it decoded the root mapping to
// read two keys and yaml.v3 abandons a mapping that repeats any key at all.
//
// Both orders are pinned: where a writer put the version key says nothing about
// what the document is, and a fixture that declares it first cannot see a
// regression that loses it to the repeats that follow.
func TestDetect_RepeatedKeysDoNotDecideTheFormat(t *testing.T) {
	t.Parallel()
	repeats := strings.Repeat("x: y\n", dupRepeats)
	cases := []struct{ name, src string }{
		{"version first", "openapi: 3.0.3\ninfo: {title: t, version: v}\n" + repeats},
		{"version last", "info: {title: t, version: v}\n" + repeats + "openapi: 3.0.3\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(tc.src)})
			assert.True(t, ok, "a document this compiler can lower must not be declined over a repeated key")
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, got)
			assert.Nil(t, codesOf(diags), "the repeats are the parser's to report, sited, not detection's")
		})
	}
}

// TestDetect_ARepeatedVersionKeyAgreesWithTheParser holds detection to the
// answer the lowering will reach. load reads the version off the parsed document
// and records it on ir.SourceInfo, and that parser takes a repeated key's last
// spelling; detection naming the first would give one document two dialects,
// one routing it and one describing it.
//
// The two orders are the test: a single order passes whichever spelling is
// taken.
func TestDetect_ARepeatedVersionKeyAgreesWithTheParser(t *testing.T) {
	t.Parallel()
	cases := []struct{ first, second, want string }{
		{"3.1.0", "3.0.3", "3.0"},
		{"3.0.3", "3.1.0", "3.1"},
	}
	for _, tc := range cases {
		t.Run(tc.first+" then "+tc.second, func(t *testing.T) {
			t.Parallel()
			src := "openapi: " + tc.first + "\nopenapi: " + tc.second + "\ninfo: {}\n"
			got, _, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(src)})
			require.True(t, ok)
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: tc.want}, got,
				"the last spelling is the one the parser reads and records")
		})
	}
}

// TestDetect_ReadsAVersionKeyThroughAMergeKey holds the merge cases the decoder
// this replaced handled for free. A root that merges another mapping declares
// what that mapping declares, and dropping it would put a new instance of "a
// field supplied through a merge key reaches the IR in no form" into the one
// place that decides whether the document is read at all.
func TestDetect_ReadsAVersionKeyThroughAMergeKey(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, want string
	}{
		{"alias", "base: &b\n  openapi: 3.1.0\n<<: *b\ninfo: {}\n", "3.1"},
		{"mapping written out", "<<: {openapi: 3.1.0}\ninfo: {}\n", "3.1"},
		{"sequence of aliases", "one: &o\n  unrelated: x\ntwo: &t\n  openapi: 3.1.0\n<<: [*o, *t]\n", "3.1"},
		{"earlier merge wins", "one: &o\n  openapi: 3.0.3\ntwo: &t\n  openapi: 3.1.0\n<<: [*o, *t]\n", "3.0"},
		{"a written key beats a merged one", "base: &b\n  openapi: 3.0.3\n<<: *b\nopenapi: 3.1.0\n", "3.1"},
		{"a quoted << merges nothing", "base: &b\n  openapi: 3.1.0\n\"<<\": *b\ninfo: {}\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(tc.src)})
			if tc.want == "" {
				assert.False(t, ok, "a key spelled << as a plain string merges nothing")
				return
			}
			require.True(t, ok)
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: tc.want}, got)
		})
	}
}

// TestSniff_BoundsAMergeChain pins the bound on the one recursion this file has.
// A merge key's value may be an alias to a mapping that merges another, so the
// chain is a property of the document and not of its size, and an anchor may
// name a mapping that reaches itself. The bound is what makes the walk finite;
// what it costs is a version key buried deeper than any document writes one.
func TestSniff_BoundsAMergeChain(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := range maxMergeDepth + 2 {
		fmt.Fprintf(&b, "l%d: &a%d\n", i, i)
		if i == 0 {
			b.WriteString("  openapi: 3.1.0\n")
			continue
		}
		fmt.Fprintf(&b, "  <<: *a%d\n", i-1)
	}
	deep := b.String() + fmt.Sprintf("<<: *a%d\n", maxMergeDepth+1)

	probe, err := sniff([]byte(deep))
	require.NoError(t, err, "a chain past the bound is declined, not failed")
	assert.Empty(t, probe.OpenAPI, "past the bound the key is not followed to")

	shallow := "l0: &a0\n  openapi: 3.1.0\n<<: *a0\n"
	probe, err = sniff([]byte(shallow))
	require.NoError(t, err)
	assert.Equal(t, "3.1.0", probe.OpenAPI, "the bound must not refuse the depth a document writes")
}

// TestDecodeYAML_RefusesARootThatIsNoMapping pins the shape complaint. A source
// with no root mapping has no top-level keys, and saying so is what lets Detect
// report bytes that declare a key it serves and will not read.
func TestDecodeYAML_RefusesARootThatIsNoMapping(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, wantErr string }{
		{"sequence", "- openapi: 3.1.0\n", "!!seq"},
		{"scalar", "just a string\n", "!!str"},
		{"empty", "", ""},
		{"comment only", "# openapi: 3.1.0\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, err := decodeYAML([]byte(tc.src))
			assert.Empty(t, probe.OpenAPI)
			if tc.wantErr == "" {
				assert.NoError(t, err, "bytes that carry no document decline rather than fail")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr, "the complaint names the shape that was read")
		})
	}
}

// TestSniff_CostIsNotQuadraticInRepeatedKeys guards the half of the defect an
// answer cannot see. Reading two keys off a parsed tree is linear in the
// document; decoding the root mapping to read them is quadratic in how often a
// key repeats, because yaml.v3 compares every pair of keys before it reads any
// of them. Both spellings answer alike on a small fixture, and only one of them
// still answers on a large one.
//
// Allocation count is the probe because it is deterministic where wall time is
// not. Doubling the repeats doubles the parse, so the bound is loose enough for
// that and nowhere near a quadratic term: measured at this size the linear
// reading grows by 1.97 and the quadratic one by 4.64.
func TestSniff_CostIsNotQuadraticInRepeatedKeys(t *testing.T) {
	head := "openapi: 3.0.3\ninfo: {}\n"
	small := []byte(head + strings.Repeat("x: y\n", dupRepeats))
	large := []byte(head + strings.Repeat("x: y\n", dupRepeats*2))

	smallAllocs := testing.AllocsPerRun(2, func() { _, _ = sniff(small) })
	largeAllocs := testing.AllocsPerRun(2, func() { _, _ = sniff(large) })

	require.Positive(t, smallAllocs, "a measurement of nothing bounds nothing")
	assert.Less(t, largeAllocs, smallAllocs*3,
		"twice the repeats must cost about twice, not about four times")
}

// TestDecodeYAML_RefusesAVersionKeyThatIsNoScalar pins the complaint for a
// version key whose value is a mapping or a sequence, written directly and
// reached through a `<<`. Such bytes name a key this compiler serves and do not
// say what dialect, which is unreadable here and nobody else's.
//
// Whether Detect reports that or declines in silence is declaresProbeKey's
// answer and not this one's, and it is asserted separately below: the guard
// reads a key at column 0, and a merged key is indented under the mapping that
// carries it.
func TestDecodeYAML_RefusesAVersionKeyThatIsNoScalar(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, wantErr string }{
		{"mapping", "openapi: {a: b}\ninfo: {}\n", "!!map"},
		{"sequence", "openapi: [3.1.0]\ninfo: {}\n", "!!seq"},
		{"merged mapping", "base: &b\n  openapi: {a: b}\n<<: *b\n", "!!map"},
		{"merged through a sequence", "base: &b\n  openapi: {a: b}\n<<: [*b]\n", "!!map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, err := decodeYAML([]byte(tc.src))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr, "the complaint names the shape that was read")
			assert.Empty(t, probe.OpenAPI)
		})
	}
}

// TestDetect_ReportsAnUnreadableVersionKeyOnlyWhereItIsDeclared pins the split
// the guard makes. A version key written at column 0 makes the source
// recognizably this compiler's, so a value that is no version is reported; the
// same value reached through a `<<` is indented under the mapping that carries
// it, which declaresProbeKey does not read, so the source is declined in silence
// instead.
//
// The silent half is deliberate and is the direction to be wrong in: the guard
// may not be widened to "the name occurs somewhere followed by a colon" without
// claiming documents of formats that nest a key of that name, and reporting
// those under this compiler's parse error is the one thing detection must not
// do.
func TestDetect_ReportsAnUnreadableVersionKeyOnlyWhereItIsDeclared(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		wantCode  []string
	}{
		{"declared at column 0", "openapi: {a: b}\ninfo: {}\n", []string{diag.UndecodableSource}},
		{"reached through a merge", "base: &b\n  openapi: {a: b}\n<<: *b\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags, ok := New().Detect(compilers.Source{Path: "api.yaml", Data: []byte(tc.src)})
			assert.False(t, ok)
			assert.Equal(t, tc.wantCode, codesOf(diags))
		})
	}
}

// TestDecodeYAML_PassesOverWhatNamesNoVersion holds the walk to reading only
// what it came for. A mapping may key an entry with a sequence, and a `<<` may
// be written with a value that merges nothing; both are the source's business
// and neither stops the two keys beside them from being read.
func TestDecodeYAML_PassesOverWhatNamesNoVersion(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src string }{
		{"a key that is a sequence", "? [a, b]\n: v\nopenapi: 3.1.0\n"},
		{"a merge of a scalar", "<<: not-a-mapping\nopenapi: 3.1.0\n"},
		{"a merge of a sequence of scalars", "<<: [x, y]\nopenapi: 3.1.0\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe, err := decodeYAML([]byte(tc.src))
			require.NoError(t, err)
			assert.Equal(t, "3.1.0", probe.OpenAPI)
		})
	}
}

// TestProbeFromMapping_StopsAtTheBound reaches the bound at the mapping rather
// than at the merge, which is the other of the two places the count is spent.
// It is called directly because the depth a chain lands on is a property of the
// chain, and pinning the bound through one is pinning the chain instead.
func TestProbeFromMapping_StopsAtTheBound(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("base: &b\n  openapi: 3.1.0\n<<: *b\n"), &root))

	spent, err := probeFromMapping(documentRoot(&root), 0)
	require.NoError(t, err, "a walk that stops at the bound declines; it does not fail")
	assert.Empty(t, spent.OpenAPI, "at the bound the merge is not followed")

	within, err := probeFromMapping(documentRoot(&root), maxMergeDepth)
	require.NoError(t, err)
	assert.Equal(t, "3.1.0", within.OpenAPI, "the same mapping within the bound is read")
}

// TestProbeFromMerge_DeclinesAnAliasThatResolvedToNothing covers the guard on an
// alias node carrying no target. A parser resolves every alias it accepts, so the
// node is built here rather than parsed: the guard exists because dereferencing
// the field is what the next line does, and a nil there is a panic in detection,
// which runs before the compiler has decided the bytes are even its own.
func TestProbeFromMerge_DeclinesAnAliasThatResolvedToNothing(t *testing.T) {
	t.Parallel()
	probe, err := probeFromMerge(&yaml.Node{Kind: yaml.AliasNode}, maxMergeDepth)
	require.NoError(t, err)
	assert.Empty(t, probe.OpenAPI)
}
