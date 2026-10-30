// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package annotation

import (
	"encoding/json/jsontext"
	"errors"
	"strconv"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	soavalidation "github.com/speakeasy-api/openapi/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestUnknownKeywordsIn_KeepsWhatTheModelDoesNotName is the census at its own
// subject: a keyword with no field in the schema model reaches the IR under its
// own key, located at itself, and is announced at info.
func TestUnknownKeywordsIn_KeepsWhatTheModelDoesNotName(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-ray: kept\nnotAKeyword: 7\n")

	var got ir.Unmodeled
	diags := UnknownKeywordsIn(&got, s, "/components/schemas/A", sourced(3))

	require.Len(t, got, 1, "the x-* is the extension reader's, not the census's; got %v", got)
	entry := got["openapi:notAKeyword"]
	assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
	assert.Equal(t, ir.RawValue("7"), entry.Value)
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/components/schemas/A/notAKeyword"}, entry.Provenance)

	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, "openapi/unknown-schema-keyword", diags[0].Code)
	assert.Contains(t, diags[0].Message, `"notAKeyword"`)
}

// TestUndefinedKeywordsIn_KeepsWhatTheCallerNames pins the census of keywords
// the model names and the dialect does not define: each one the caller names is
// kept under its own key, located at itself and announced at info as an unknown
// keyword, whatever its value, and naming none keeps nothing.
func TestUndefinedKeywordsIn_KeepsWhatTheCallerNames(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nnullable: false\n")

	var got ir.Unmodeled
	diags := UndefinedKeywordsIn(&got, s, []string{"nullable"}, "/components/schemas/A", sourced(2))

	require.Len(t, got, 1)
	entry := got["openapi:nullable"]
	assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
	assert.Equal(t, ir.RawValue("false"), entry.Value)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/A/nullable"}, entry.Provenance)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityInfo, diags[0].Severity)
	assert.Equal(t, diag.UnknownSchemaKeyword, diags[0].Code)
	assert.Contains(t, diags[0].Message, `"nullable" is not defined by the dialect`)

	var none ir.Unmodeled
	assert.Empty(t, UndefinedKeywordsIn(&none, s, nil, "/components/schemas/A", sourced(2)))
	assert.Empty(t, none)
}

// TestUnknownKeywordsIn_DecidedKeywordsAreLeftAlone holds the census to the
// decisions already recorded elsewhere. Each of these has no field in the schema
// model, so the census would otherwise claim all three and overrule a
// deliberate drop — or, for an expanded $dynamicRef, say the compiler ignored a
// reference it resolved.
func TestUnknownKeywordsIn_DecidedKeywordsAreLeftAlone(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"$comment", "$dynamicAnchor", "$dynamicRef"}, DecidedKeywords,
		"a keyword joining or leaving the exclusion must be decided here too")

	var body strings.Builder
	body.WriteString("type: string\n")
	for _, keyword := range DecidedKeywords {
		body.WriteString(keyword + ": v\n")
	}
	s := schemaFromYAML(t, body.String())

	var got ir.Unmodeled
	diags := UnknownKeywordsIn(&got, s, "/components/schemas/A", sourced(0))

	assert.Empty(t, got, "each is decided about elsewhere, so the census keeps none of them")
	assert.Empty(t, diags)
}

// TestUnknownKeywordsIn_AlreadyRecordedKeyIsLeftAlone is why the census runs
// last. dependentRequired has no field in the model either and is kept by the
// validation-only reader under a reason that says what it is; a census that
// overwrote it would replace that with a weaker one and announce the keyword
// twice.
func TestUnknownKeywordsIn_AlreadyRecordedKeyIsLeftAlone(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\ndependentRequired: {a: [b]}\n")
	already := ir.UnmodeledEntry{
		Reason:     ir.ReasonValidationOnly,
		Value:      ir.RawValue(`{"a":["b"]}`),
		Provenance: ir.Provenance{Pointer: "/A/dependentRequired"},
	}
	got := ir.Unmodeled{"openapi:dependentRequired": already}

	diags := UnknownKeywordsIn(&got, s, "/A", sourced(0))

	assert.Equal(t, ir.Unmodeled{"openapi:dependentRequired": already}, got)
	assert.Empty(t, diags, "a keyword another reader already announced is not announced twice")
}

// TestUnknownKeywordsIn_UnpreservableValueIsReportedNotKept holds the census to
// GitHub #144's rule: a value that cannot be rendered as JSON keeps nothing, and
// says so, rather than announcing a preservation that did not happen.
func TestUnknownKeywordsIn_UnpreservableValueIsReportedNotKept(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nnotAKeyword: .nan\n")

	var got ir.Unmodeled
	diags := UnknownKeywordsIn(&got, s, "/A", sourced(0))

	assert.Empty(t, got)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, "openapi/unpreservable-construct", diags[0].Code)
}

// TestUnknownKeysUnder_KeysBeneathTheScopeAndSorted pins the OpenAPI-object
// class: entries key under the path that says which object wrote them, the
// grading is a warning because the format admits no such key, and both the
// entries and the findings come out in key order.
//
// Sorted matters on its own. The library builds its census from a parallel walk
// of the mapping, so the order it hands back is neither source order nor stable,
// and diagnostics ordered by it would make the document non-deterministic
// (invariant 7).
func TestUnknownKeysUnder_KeysBeneathTheScopeAndSorted(t *testing.T) {
	t.Parallel()
	reported := []string{"zeta", "alpha"}
	obj := fakeObject{core: &fakeCore{keys: reported}, root: openapitest.YAMLDocument(t, "zeta: 1\nalpha: 2\n")}

	var got ir.Unmodeled
	diags := UnknownKeysUnder(&got, obj, sourced(1), "/info/contact", "info/contact")

	assert.Equal(t, []string{"zeta", "alpha"}, reported, "the model's own slice is not reordered")
	require.Len(t, got, 2)
	assert.Equal(t, ir.RawValue("2"), got["openapi:info/contact/alpha"].Value)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/info/contact/zeta"},
		got["openapi:info/contact/zeta"].Provenance)

	require.Len(t, diags, 2)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, "openapi/unknown-object-key", diags[0].Code)
	assert.Contains(t, diags[0].Message, `"alpha"`, "the findings follow the sorted keys")
	assert.Contains(t, diags[1].Message, `"zeta"`)
}

// TestUnknownKeysIn_AttributesEachKeyAtItsOwnPointer pins GitHub #522: two
// undeclared keys on the same owner are located independently, so an overlay
// that rewrites one leaves the other with the base — in both the kept entry
// and the diagnostic announcing it.
func TestUnknownKeysIn_AttributesEachKeyAtItsOwnPointer(t *testing.T) {
	t.Parallel()
	obj := fakeObject{core: &fakeCore{keys: []string{"alpha", "zeta"}}, root: openapitest.YAMLDocument(t, "alpha: 1\nzeta: 2\n")}

	var got ir.Unmodeled
	diags := UnknownKeysIn(&got, obj, overlaid("/x/zeta"), "/x")

	require.Len(t, got, 2)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/x/alpha"}, got["openapi:alpha"].Provenance,
		"the untouched key stays with the base")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/x/zeta"}, got["openapi:zeta"].Provenance,
		"the overlaid key names the overlay")

	require.Len(t, diags, 2)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/x/alpha"}, diags[0].Provenance,
		"sorted keys put alpha first")
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/x/zeta"}, diags[1].Provenance)
}

// TestUnknownKeysIn_BudgetBoundsWhatOneObjectContributes exercises the bound. An
// object writing more undeclared keys than MaxUnknownKeys keeps exactly that
// many and reports the remainder, which is what stops the discarded tail from
// being the silent loss this census exists to end.
func TestUnknownKeysIn_BudgetBoundsWhatOneObjectContributes(t *testing.T) {
	t.Parallel()
	const over = MaxUnknownKeys + 3
	keys := make([]string, 0, over)
	var body strings.Builder
	for i := range over {
		// Zero-padded, so sorting the census is sorting the source order too and the
		// key that survives the truncation is a predictable one.
		key := "k" + strconv.Itoa(1000+i)
		keys = append(keys, key)
		body.WriteString(key + ": " + strconv.Itoa(i) + "\n")
	}
	obj := fakeObject{core: &fakeCore{keys: keys}, root: openapitest.YAMLDocument(t, body.String())}

	var got ir.Unmodeled
	diags := UnknownKeysIn(&got, obj, sourced(0), "/x")

	assert.Len(t, got, MaxUnknownKeys)
	assert.NotContains(t, got, "openapi:k"+strconv.Itoa(1000+over-1), "the tail past the bound is dropped")
	require.NotEmpty(t, diags)
	assert.Equal(t, "openapi/unknown-key-budget", diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, ir.Provenance{Pointer: "/x"}, diags[0].Provenance)
}

// TestUnknownKeysIn_KeyWithNoSourceNodeIsReported covers the one outcome
// PreserveNodeInto's contract does not: a key whose value node is not in the
// mapping.
//
// Everywhere else an absent node means the construct was never written, which is
// why it records nothing and says nothing. Here the parser has already reported
// the key as present, so an absent node means this reader could not reach one
// the document does write — a merged-in key, in practice — and passing over it
// would be the silent loss this census exists to end (GitHub #395).
func TestUnknownKeysIn_KeyWithNoSourceNodeIsReported(t *testing.T) {
	t.Parallel()
	obj := fakeObject{core: &fakeCore{keys: []string{"absent"}}, root: openapitest.YAMLDocument(t, "present: 1\n")}

	var got ir.Unmodeled
	diags := UnknownKeysIn(&got, obj, sourced(2), "/x")

	assert.Empty(t, got, "there was no node to read")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, "openapi/unknown-key-unreachable", diags[0].Code)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/x/absent"}, diags[0].Provenance)
}

// TestUnknownKeysUnder_KeyHoldingASeparatorIsItsOwnEntry pins the escaping that
// keeps a key from spelling another object's scope. Both keys below reach the
// same carrier, and unescaped both key under "openapi:info/contact/slack": the
// second site found the first already recorded, took the branch meant for a
// keyword another reader kept, and dropped a key with no diagnostic at all.
func TestUnknownKeysUnder_KeyHoldingASeparatorIsItsOwnEntry(t *testing.T) {
	t.Parallel()
	root := fakeObject{
		core: &fakeCore{keys: []string{"info/contact/slack"}},
		root: openapitest.YAMLDocument(t, "info/contact/slack: fromRoot\n"),
	}
	contact := fakeObject{
		core: &fakeCore{keys: []string{"slack"}},
		root: openapitest.YAMLDocument(t, "slack: fromContact\n"),
	}

	var got ir.Unmodeled
	diags := UnknownKeysUnder(&got, root, sourced(0), "", "")
	diags = append(diags, UnknownKeysUnder(&got, contact, sourced(0), "/info/contact", "info/contact")...)

	assert.Equal(t, ir.RawValue(`"fromRoot"`), got["openapi:info~1contact~1slack"].Value,
		"the root's key is one segment, escaped")
	assert.Equal(t, ir.RawValue(`"fromContact"`), got["openapi:info/contact/slack"].Value,
		"the scoped entry is the contact object's, and the root cannot spell it")
	assert.Len(t, diags, 2, "two keys survive, so two are announced")
}

// TestUnknownKeysIn_EntryHeldByAnotherConstructIsReported separates the two
// reasons an entry can already exist, which the census used to treat alike.
//
// A reader with more to say about a keyword writes it at the pointer the census
// would use, and there the census has nothing to add and says nothing —
// $vocabulary and dependentRequired on a schema's own map. An entry pointing
// somewhere else belongs to a different construct that happens to spell the same
// key, so the key this census holds is lost; it is announced instead of skipped
// (GitHub #396).
func TestUnknownKeysIn_EntryHeldByAnotherConstructIsReported(t *testing.T) {
	t.Parallel()
	obj := fakeObject{core: &fakeCore{keys: []string{"same", "other"}}, root: openapitest.YAMLDocument(t, "same: 1\nother: 2\n")}
	got := ir.Unmodeled{
		"openapi:same": {Reason: ir.ReasonValidationOnly, Value: ir.RawValue("9"),
			Provenance: ir.Provenance{Pointer: "/x/same"}},
		"openapi:other": {Reason: ir.ReasonNoIRHome, Value: ir.RawValue("9"),
			Provenance: ir.Provenance{Pointer: "/x/schema/other"}},
	}

	diags := UnknownKeysIn(&got, obj, sourced(0), "/x")

	assert.Equal(t, ir.RawValue("9"), got["openapi:same"].Value, "neither entry is overwritten")
	assert.Equal(t, ir.RawValue("9"), got["openapi:other"].Value)
	require.Len(t, diags, 1, "the key recorded for this very construct is not announced again")
	assert.Equal(t, "openapi/unknown-key-entry-taken", diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Equal(t, jsontext.Pointer("/x/other"), diags[0].Provenance.Pointer)
	assert.Contains(t, diags[0].Message, "/x/schema/other", "the holder is named, so the clash is findable")
}

// TestUnknownKeysIn_ModelWithNoCensusRecordsNothing covers the shapes the reader
// must survive rather than panic on. The absent object is the one that occurs:
// the getters hand back a typed nil for an object the document omitted, and a
// promoted method on one of those dereferences it.
func TestUnknownKeysIn_ModelWithNoCensusRecordsNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		model any
	}{
		{"an object the document omitted", (*fakeObjectPtr)(nil)},
		{"an untyped nil", nil},
		{"a value that is no parsed model", 42},
		{"a model whose core keeps no census", fakeObject{core: "not a core"}},
		{"a model with an empty census", fakeObject{core: &fakeCore{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got ir.Unmodeled

			diags := UnknownKeysIn(&got, tc.model, sourced(0), "/x")

			assert.Nil(t, got)
			assert.Empty(t, diags)
		})
	}
}

// TestUnknownKeysNamed_DelegatesTheSameGrading covers the object census's other
// entry point: an object whose model keeps no key list of its own — a Path
// Item's leftovers, in practice — so the caller names the keys the model does
// not define and this reader grades them exactly as the modelled path does.
func TestUnknownKeysNamed_DelegatesTheSameGrading(t *testing.T) {
	t.Parallel()
	root := openapitest.YAMLDocument(t, "get: {}\nnotAKey: 3\n")

	var got ir.Unmodeled
	diags := UnknownKeysNamed(&got, []string{"notAKey"}, root, sourced(0), "/paths/~1p", "")

	require.Len(t, got, 1, "got %v", got)
	entry := got["openapi:notAKey"]
	assert.Equal(t, ir.ReasonOutOfScope, entry.Reason)
	assert.Equal(t, ir.RawValue("3"), entry.Value)
	assert.Equal(t, ir.Provenance{Pointer: "/paths/~1p/notAKey"}, entry.Provenance)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnknownObjectKey, diags[0].Code)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
}

// fakeObject is a parsed model standing in for the library's, so the census can
// be driven at shapes no real document produces — an unsorted census, one past
// the bound, and a core that keeps none.
type fakeObject struct {
	core any
	root *yaml.Node
}

func (f fakeObject) GetCoreAny() any         { return f.core }
func (f fakeObject) GetRootNode() *yaml.Node { return f.root }

// fakeObjectPtr is fakeObject's pointer-receiver twin, for the typed-nil case:
// a promoted method on a nil pointer is what the guard exists for, and a
// value-receiver method on a nil pointer would not reach it.
type fakeObjectPtr struct{ fakeObject }

// fakeCore is a core model's census, reported exactly as given.
type fakeCore struct{ keys []string }

func (c *fakeCore) GetUnknownProperties() []string { return c.keys }

// TestTakeString_UnkeepableValueIsReportedAndRead pins the one value TakeString
// can neither place nor keep: a mapping that writes one key twice, which no JSON
// rendering can hold. It is reported as lost, and still read, so the census does
// not report it a second time.
func TestTakeString_UnkeepableValueIsReportedAndRead(t *testing.T) {
	t.Parallel()
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("k: {a: 1, a: 2}"), &root))
	var p ir.Unmodeled
	got, read, diags := TakeString(&p, &root, "k", nil, sourced(0), "/o", "")
	assert.Empty(t, got)
	assert.True(t, read)
	assert.Empty(t, p)
	require.Len(t, diags, 1)
	assert.Equal(t, diag.UnpreservableConstruct, diags[0].Code)
}

// parserString returns what the parser makes of value written as the optional
// string field info.summary, which the model names where TakeString reads one it
// does not: the string it decodes, empty where the field is null or unset, and
// whether it decodes the value as a string at all. The anchor x stands behind an
// alias.
func parserString(t *testing.T, value string) (string, bool) {
	t.Helper()
	src := "openapi: 3.1.0\nx-a: &x hello\ninfo:\n  title: T\n  version: '1'\n  summary: " + value + "\npaths: {}\n"
	parsed, findings, err := soa.Unmarshal(t.Context(), strings.NewReader(src))
	require.NoError(t, err)
	for _, e := range findings {
		var ve *soavalidation.Error
		if errors.As(e, &ve) && ve.Rule == soavalidation.RuleValidationTypeMismatch {
			return "", false
		}
	}
	return parsed.Info.GetSummary(), true
}

// TestTakeString_DecodesAsTheParserDoes holds TakeString to the parser's own
// decoding of a string field: each value is written as a field the model names
// and the parser decodes it, or reports it a type mismatch, and TakeString reads
// the same node as the parser does, the string where the parser decodes one
// and the value verbatim at the census's own entry, with a warning, where it
// does not. The want and decodes columns pin today's parser.
func TestTakeString_DecodesAsTheParserDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
		want        string
		decodes     bool
		kept        string // the verbatim value kept, "" for none
	}{
		{"a string", "plain", "plain", true, ""},
		{"an int", "5", "5", true, ""},
		{"a float", "1.5", "1.5", true, ""},
		{"a bool", "true", "true", true, ""},
		{"an empty string", `""`, "", true, ""},
		{"null", "~", "", true, ""},
		{"an alias", "*x", "hello", true, ""},
		{"an explicit int tag", "!!int 5", "5", true, ""},
		{"a sequence", "[a, b]", "", false, `["a","b"]`},
		{"a mapping", "{a: b}", "", false, `{"a":"b"}`},
		{"a timestamp", "2001-12-14", "", false, `"2001-12-14"`},
		{"a local tag", "!local v", "", false, `"v"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parsed, decodes := parserString(t, tc.value)
			assert.Equal(t, tc.want, parsed, "what the parser decodes")
			assert.Equal(t, tc.decodes, decodes, "whether the parser decodes it as a string")

			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte("a: &x hello\nk: "+tc.value), &root))
			var p ir.Unmodeled
			got, read, diags := TakeString(&p, &root, "k", nil, sourced(0), "/o", "s")
			assert.Equal(t, parsed, got)
			assert.True(t, read)
			if decodes {
				assert.Empty(t, p)
				assert.Empty(t, diags)
				return
			}
			entry, ok := p["openapi:s/k"]
			require.True(t, ok)
			assert.JSONEq(t, tc.kept, string(entry.Value))
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Equal(t, jsontext.Pointer("/o/k"), entry.Provenance.Pointer)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
			assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
		})
	}
}

// TestTakeString_AppliesValidAndReadsAsTheParserDoes pins what TakeString
// decides itself, whatever the parser decodes: a non-empty value valid refuses
// is kept verbatim as a value that does not decode is, an empty or null one is
// not put to valid, a key merged in through `<<` is read as one written there
// is, and a key the mapping does not present, absent or merged in from past
// nodeview.MergeDepthLimit, is left unread for the census.
func TestTakeString_AppliesValidAndReadsAsTheParserDoes(t *testing.T) {
	t.Parallel()
	only := func(v string) bool { return v == "ok" }
	for _, tc := range []struct {
		name  string
		src   string
		valid func(string) bool
		want  string
		read  bool
		kept  string // the verbatim value kept, "" for none
	}{
		{"a value valid accepts", "k: ok", only, "ok", true, ""},
		{"a value valid refuses", "k: no", only, "", true, `"no"`},
		{"an empty string valid would refuse", `k: ""`, only, "", true, ""},
		{"a null valid would refuse", "k: ~", only, "", true, ""},
		{"absent", "j: x", nil, "", false, ""},
		{"merged in", "<<: {k: m}", nil, "m", true, ""},
		{"merged in past the bound", "<<: " + pastMergeBound("{k: m}"), nil, "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var root yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tc.src), &root))
			var p ir.Unmodeled
			got, read, diags := TakeString(&p, &root, "k", tc.valid, sourced(0), "/o", "s")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.read, read)
			if tc.kept == "" {
				assert.Empty(t, p)
				assert.Empty(t, diags)
				return
			}
			entry, ok := p["openapi:s/k"]
			require.True(t, ok)
			assert.JSONEq(t, tc.kept, string(entry.Value))
			assert.Equal(t, ir.ReasonDegradedLowering, entry.Reason)
			assert.Equal(t, jsontext.Pointer("/o/k"), entry.Provenance.Pointer)
			require.Len(t, diags, 1)
			assert.Equal(t, diag.DegradedConstruct, diags[0].Code)
			assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
		})
	}
}
