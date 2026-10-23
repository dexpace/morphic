package detect

import (
	"fmt"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestRecognize_HoldsTheParseToItsByteBudget pins the bound on what recognition
// reads: the parse is of the whole source, so a source past maxBytes is
// declined unread, with the refusal load.Load gives the same source. One at
// the budget is read, and so is any source under no budget at all.
func TestRecognize_HoldsTheParseToItsByteBudget(t *testing.T) {
	t.Parallel()
	const src = "openapi: 3.1.0\ninfo: {title: T, version: \"1\"}\npaths: {}\n"
	refusal, over := load.OverByteBudget(ir.Provenance{Source: 0}, []byte(src), len(src)-1)
	require.True(t, over, "the refusal the case expects")

	rec, diags, ok := Recognize(0, []byte(src), len(src)-1)
	assert.False(t, ok)
	assert.Equal(t, compilers.Recognition{}, rec, "a source declined unread carries no parse")
	assert.Equal(t, []ir.Diagnostic{refusal}, diags, "the refusal Load gives the same source")

	for _, budget := range []int{len(src), 0} {
		rec, diags, ok := Recognize(0, []byte(src), budget)
		require.True(t, ok, "budget %d: %v", budget, diags)
		assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.1"}, rec.Format, "budget %d", budget)
	}
}

// TestRecognize_ADeclineNamesTheSourceItWasHanded holds every decline that
// says why to the index its caller passed. The compiler's Detect passes 0, so
// a recognition that named source 0 whatever it was handed would pass every
// other test here.
func TestRecognize_ADeclineNamesTheSourceItWasHanded(t *testing.T) {
	t.Parallel()
	const srcIndex = 3
	cases := map[string]struct {
		src      []byte
		maxBytes int
		code     string
	}{
		"past the byte budget": {[]byte("openapi: 3.1.0\n"), 1, diag.BudgetExceeded},
		"merged past the depth the read expands": {
			openapitest.RootMergeChain(nodeview.MergeDepthLimit+1, "openapi: 3.1.0", ""), 0, diag.BudgetExceeded},
		"declaring a version and unreadable": {[]byte("openapi: 3.1.0\ninfo: [unterminated\n"), 0, diag.UndecodableSource},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, diags, ok := Recognize(srcIndex, tc.src, tc.maxBytes)
			assert.False(t, ok)
			require.Len(t, diags, 1)
			assert.Equal(t, tc.code, diags[0].Code)
			assert.Equal(t, srcIndex, diags[0].Provenance.Source)
		})
	}
}

func TestRecognize_Formats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      compilers.SourceFormat
		wantOK    bool
		// wantCode is the codes of the diagnostics a decline carries. Empty for
		// every source this compiler recognizes, and for every source that is
		// another format's business.
		wantCode []string
	}{
		{"openapi 3.1 yaml", "openapi: 3.1.0\ninfo: {}\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true, nil},
		{"openapi 3.0 json", `{"openapi": "3.0.3"}`,
			compilers.SourceFormat{Name: "openapi", Version: "3.0"}, true, nil},
		{"openapi 3.2 yaml", "openapi: 3.2.0\ninfo: {}\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.2"}, true, nil},
		// A version already in major.minor form (single dot) exercises
		// majorMinor's unchanged-passthrough return.
		{"openapi major.minor only", "openapi: \"3.1\"\n",
			compilers.SourceFormat{Name: "openapi", Version: "3.1"}, true, nil},
		// A bare-major version (no dot) also reaches the passthrough return.
		{"openapi bare major", "openapi: \"4\"\n",
			compilers.SourceFormat{Name: "openapi", Version: "4"}, true, nil},
		// Recognized, and deliberately not a format Formats reports: naming it
		// lets the caller say "unsupported" rather than "unreadable".
		{"swagger", "swagger: \"2.0\"\n",
			compilers.SourceFormat{Name: "swagger", Version: "2.0"}, true, nil},

		// Everything below is another format's input, and none of it is an error
		// here: three of the five planned compilers take bytes that are not YAML.
		{"protobuf", "syntax = \"proto3\";\nservice S { rpc Get (Q) returns (A); }\n",
			compilers.SourceFormat{}, false, nil},
		{"typespec", "import \"@typespec/http\";\nmodel Pet { name: string; }\n",
			compilers.SourceFormat{}, false, nil},
		{"graphql", "type Query {\n  pet(id: ID!): Pet\n}\n",
			compilers.SourceFormat{}, false, nil},
		// Valid YAML that declares neither key: whether a source parses is not
		// the question detection answers.
		{"graphql that parses as yaml", "type Query { a: String }\n",
			compilers.SourceFormat{}, false, nil},
		{"yaml that is no spec", "hello: world\n",
			compilers.SourceFormat{}, false, nil},
		// Declares a version and will not parse: this compiler's own source,
		// broken, which nothing else is in a position to say.
		{"unparseable yaml", "openapi: 3.1.0\ninfo: [unterminated\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// Broken on its own version line, so it declares no version to read, and
		// is declined like any file that does not say what it is. That is the
		// price of not claiming prose (GitHub #497).
		{"unparseable yaml, broken on the version line", "openapi: [unterminated\n",
			compilers.SourceFormat{}, false, nil},
		// The two files #497 is about: the word written where a spec declares its
		// version, with no version beside it. A README with prose after the key
		// and a framework's configuration section under it are other formats'
		// files, and neither is reported as a spec that failed to read.
		{"a readme that does not parse", "# Notes\n\nfiller text\nopenapi: is a format\n",
			compilers.SourceFormat{}, false, nil},
		{"a config section that does not parse", "openapi:\n  enabled: true\nserver: [\n",
			compilers.SourceFormat{}, false, nil},
		{"unparseable json", `{"openapi": "3.1.0", "info": {`,
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// A key that is not UTF-8 refuses the whole document, which is what lets
		// ids.Ptr assume every token it escapes is UTF-8.
		{"undecodable yaml key", "openapi: 3.1.0\ninfo: {}\n\xff: x\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		{"undecodable json key", "{\"openapi\": \"3.1.0\", \"\xff\": 1}",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// Broken, and never this compiler's: the key it names is a value, not a
		// key, so the parse error describes a parser that was wrong to be asked.
		{"unparseable, key only mentioned", "syntax = \"openapi\";\n{[",
			compilers.SourceFormat{}, false, nil},
		// A large document is parsed as a small one is, so one that declares the
		// key and will not parse is reported at any size. Detection used to scan
		// past 64 KiB instead, and a scan cannot tell a broken spec from another
		// format's file naming the word, so the same broken spec drew a complaint
		// or silence depending on its length (GitHub #486).
		{"unparseable, large",
			padTo("openapi: 3.1.0\ninfo: [unterminated\n", "filler: x\n"),
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// The key written behind a prefix that does not parse. The scan read past
		// the prefix to the version and named the format, leaving the compile to
		// find the document broken; the parse finds it broken here, and the
		// version declared at column 0 makes it this compiler's to say so.
		{"key behind an unparseable prefix, large",
			padTo("bad: [unterminated\n", "filler: x\n") + "openapi: 3.1.0\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// yaml.v3 refuses a document that follows an explicit end marker without
		// a `---` of its own, whatever the document's size.
		{"key after an end-marked empty document",
			"---\n...\nopenapi: 3.1.0\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		{"key after an end-marked empty document, large",
			"---\n...\n" + padTo("openapi: 3.1.0\n", "filler: x\n"),
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		{"key behind an unterminated quoted scalar, large",
			padTo("bad: \"unterminated\n", "filler: x\n") + "openapi: 3.1.0\n",
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// The same in flow style: the key is one of the root mapping's own
		// entries, which is what makes a broken document this compiler's.
		{"key behind an unparseable flow prefix, large",
			`{"pad":"` + flowPad() + `","bad" 1,"openapi":"3.1.0"}`,
			compilers.SourceFormat{}, false, []string{diag.UndecodableSource}},
		// Another format's document, large, naming the word as a key and broken
		// besides. It opens no mapping of its own, so the key is not its
		// declaration of itself and this compiler has nothing to say: reporting a
		// parse error here would claim bytes that were never its own.
		{"a broken document of another format names the key",
			`[{"openapi":"3.1.0"},"` + flowPad() + `"`,
			compilers.SourceFormat{}, false, nil},
		// The same, one level down inside a mapping that does open the document.
		// A nested key names a field, not the format of the file holding it.
		{"a broken document of another format nests the key",
			`{"pad":"` + flowPad() + `","deep":{"openapi":"3.1.0"},"bad" 1}`,
			compilers.SourceFormat{}, false, nil},
		{"empty", "", compilers.SourceFormat{}, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := recognize(tc.src)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got.Format)
			assert.Equal(t, tc.wantCode, codesOf(diags),
				"a decline says something only when the source is recognizably this compiler's")
		})
	}
}

// TestRecognize_KeyOrderDoesNotDecideTheFormat pins a published spec whose
// `components` object runs long and whose `openapi` key sits behind it. A JSON
// object's keys are unordered, so the same document written with its version
// key first and with it last is one document, and detection has to answer the
// same for both. A prefix-only sniff once declined the version-last spellings.
func TestRecognize_KeyOrderDoesNotDecideTheFormat(t *testing.T) {
	t.Parallel()
	flow, block := bigComponents()
	cases := []struct{ name, src string }{
		{"flow json, version first", `{"openapi":"3.0.3",` + flow + `}`},
		{"flow json, version last", `{` + flow + `,"openapi":"3.0.3"}`},
		{"block yaml, version first", "openapi: 3.0.3\n" + block},
		{"block yaml, version last", block + "openapi: 3.0.3\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Greater(t, len(tc.src), scanCutover, "the case must be large to test it")
			got, diags, ok := recognize(tc.src)
			assert.True(t, ok, "a valid document must not be declined over where it declares its version")
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, got.Format)
			assert.Nil(t, codesOf(diags), "a document this compiler recognizes carries no complaint")
		})
	}
}

// bigComponents returns a `components` entry whose value alone runs past
// scanCutover, in flow and in block style. It stands in for the schema catalogue a
// published spec leads with; what matters is only that it is one entry too large
// to read past.
func bigComponents() (flow, block string) {
	var f, b strings.Builder
	f.WriteString(`"components":{"schemas":{`)
	b.WriteString("components:\n  schemas:\n")
	for i := 0; f.Len() <= scanCutover || b.Len() <= scanCutover; i++ {
		if i > 0 {
			f.WriteByte(',')
		}
		fmt.Fprintf(&f, `"S%d":{"type":"object","description":"a schema"}`, i)
		fmt.Fprintf(&b, "    S%d:\n      type: object\n      description: a schema\n", i)
	}
	f.WriteString(`}}`)
	return f.String(), b.String()
}

// TestRecognize_RepeatedKeysDoNotDecideTheFormat pins the fix for the blow-up. A
// document that repeats a top-level key has a duplicate key, which the parser
// this compiler goes on to use reports once per repeat, sited. It is neither
// another format's nor unreadable, yet decoding the root mapping to read two
// keys said both, because yaml.v3 abandons a mapping that repeats any key.
//
// Both orders are pinned: a fixture that declares the version first cannot see
// a regression that loses it to the repeats that follow.
func TestRecognize_RepeatedKeysDoNotDecideTheFormat(t *testing.T) {
	t.Parallel()
	repeats := strings.Repeat("x: y\n", dupRepeats)
	cases := []struct{ name, src string }{
		{"version first", "openapi: 3.0.3\ninfo: {title: t, version: v}\n" + repeats},
		{"version last", "info: {title: t, version: v}\n" + repeats + "openapi: 3.0.3\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, diags, ok := recognize(tc.src)
			assert.True(t, ok, "a document this compiler can lower must not be declined over a repeated key")
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: "3.0"}, got.Format)
			assert.Nil(t, codesOf(diags), "the repeats are the parser's to report, sited, not detection's")
		})
	}
}

// TestRecognize_ARepeatedVersionKeyAgreesWithTheParser holds detection to the
// answer the lowering will reach. load reads the version off the parsed document
// and records it on ir.SourceInfo, so each case asks that parser which spelling
// of a repeated key it keeps; detection naming another would give one document
// two dialects, one routing it and one describing it.
//
// The two orders are the test: a single order passes whichever spelling is
// taken.
func TestRecognize_ARepeatedVersionKeyAgreesWithTheParser(t *testing.T) {
	t.Parallel()
	cases := []struct{ first, second, want string }{
		{"3.1.0", "3.0.3", "3.0"},
		{"3.0.3", "3.1.0", "3.1"},
	}
	for _, tc := range cases {
		t.Run(tc.first+" then "+tc.second, func(t *testing.T) {
			t.Parallel()
			src := "openapi: " + tc.first + "\nopenapi: " + tc.second + "\ninfo: {}\n"
			parsed, _, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
			require.NoError(t, err)
			require.NotNil(t, parsed)
			assert.Equal(t, tc.want, majorMinor(parsed.OpenAPI), "the spelling the parser keeps")

			got, _, ok := recognize(src)
			require.True(t, ok)
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: majorMinor(parsed.OpenAPI)}, got.Format,
				"detection names the version the parser records")
		})
	}
}

// TestRecognize_ReadsAVersionKeyThroughAMergeKey holds the merge cases the decoder
// this replaced handled for free. A root that merges another mapping declares
// what that mapping declares, and dropping it would put a new instance of "a
// field supplied through a merge key reaches the IR in no form" into the one
// place that decides whether the document is read at all.
func TestRecognize_ReadsAVersionKeyThroughAMergeKey(t *testing.T) {
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
			got, _, ok := recognize(tc.src)
			if tc.want == "" {
				assert.False(t, ok, "a key spelled << as a plain string merges nothing")
				return
			}
			require.True(t, ok)
			assert.Equal(t, compilers.SourceFormat{Name: "openapi", Version: tc.want}, got.Format)
		})
	}
}

// TestRecognize_AReadCutShortIsReported pins that the bound never passes for a
// decline: the source may be this compiler's own, so detection says why it
// could not tell. That holds whatever the chain holds, a version or another
// format's keys, since what lies past the bound is what went unread.
func TestRecognize_AReadCutShortIsReported(t *testing.T) {
	t.Parallel()
	for name, leaf := range map[string]string{
		"a version past the bound":      "openapi: 3.1.0",
		"another format past the bound": "kind: Deployment",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := openapitest.RootMergeChain(nodeview.MergeDepthLimit+1, leaf, "")
			_, diags, ok := recognize(string(src))

			assert.False(t, ok)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.BudgetExceeded, diags[0].Code)
			assert.Equal(t, ir.SeverityError, diags[0].Severity, "it is the whole reason the source went unclaimed")
			assert.Contains(t, diags[0].Message, fmt.Sprint(nodeview.MergeDepthLimit))
		})
	}
}

// TestRecognize_AMergeCycleHidesNoKey pins the other way a root's merges can stop
// short of a complete expansion: a cycle, which the compile's alias pre-scan
// refuses and detection meets first. It reads every mapping on it, so it cuts
// no read short, and a root that declares nothing is declined in silence as
// any other file that is not a spec is, while one that declares a version is
// claimed for the compile to refuse.
func TestRecognize_AMergeCycleHidesNoKey(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		src     string
		version string
	}{
		"a root merging itself":                        {"&r\n<<: *r\nfoo: 1\n", ""},
		"a root merging itself beside its version":     {"&r\n<<: *r\nopenapi: 3.1.0\n", "3.1"},
		"a root merging a mapping that merges it back": {"&r\nx: &x {<<: *r, openapi: 3.0.3}\n<<: *x\n", "3.0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe, _, err := sniff([]byte(tc.src))
			require.NoError(t, err)
			assert.False(t, probe.Cut, "a cycle reads every mapping on it")

			rec, diags, ok := recognize(tc.src)
			assert.Equal(t, tc.version != "", ok)
			assert.Equal(t, tc.version, rec.Format.Version)
			assert.Empty(t, diags, "a cycle is no budget, and a file declaring nothing earns no complaint")
		})
	}
}

// TestRecognize_AVersionKeyOfTheWrongShapeIsNoDeclaration pins that a mapping
// where a version goes declares no version, wherever it sits. Such a document
// parses, so it is the tree and not the guard that has the key — but a mapping
// under `openapi` is what another tool's configuration writes, and a spec
// written that way has no version to route it by either. Detection used to
// report the column-0 spelling as this compiler's own and unreadable; it
// declines all three alike now (GitHub #497).
func TestRecognize_AVersionKeyOfTheWrongShapeIsNoDeclaration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		wantCode  []string
	}{
		{"declared at column 0", "openapi: {a: b}\ninfo: {}\n", nil},
		{"a framework's configuration section", "openapi:\n  path: /docs\n  enabled: true\n", nil},
		{"reached through a merge", "base: &b\n  openapi: {a: b}\n<<: *b\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags, ok := recognize(tc.src)
			assert.False(t, ok)
			assert.Equal(t, tc.wantCode, codesOf(diags))
		})
	}
}
