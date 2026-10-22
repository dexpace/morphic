package annotation

import (
	"encoding/json/jsontext"
	"slices"
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// TestCombinedRaw_JoinsOnlyWhatIsWritten pins the three combining readers. Each
// keyword present goes into one object in the requested order, and a
// combination with nothing written yields nothing rather than an empty object.
// The keywords returned beside it are exactly the ones it holds, since those are
// what the entry's provenance is asked about (GitHub #534).
func TestCombinedRaw_JoinsOnlyWhatIsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		read func(*oas3.Schema) (ir.RawValue, []string, error)
		want string
		keys []string
	}{
		{
			name: "every if/then/else arm", read: IfThenElseRaw,
			body: "if: {type: string}\nthen: {maxLength: 2}\nelse: {type: integer}\n",
			want: `{"if":{"type":"string"},"then":{"maxLength":2},"else":{"type":"integer"}}`,
			keys: []string{"if", "then", "else"},
		},
		{
			name: "only the arms written", read: IfThenElseRaw,
			body: "if: {type: string}\nelse: {type: integer}\n",
			want: `{"if":{"type":"string"},"else":{"type":"integer"}}`,
			keys: []string{"if", "else"},
		},
		{name: "no arms at all", read: IfThenElseRaw, body: "type: string\n"},
		{
			name: "contains with both bounds", read: ContainsRaw,
			body: "contains: {type: string}\nminContains: 1\nmaxContains: 3\n",
			want: `{"contains":{"type":"string"},"minContains":1,"maxContains":3}`,
			keys: []string{"contains", "minContains", "maxContains"},
		},
		{name: "no contains family", read: ContainsRaw, body: "type: array\n"},
		{
			name: "both unevaluated keywords", read: UnevaluatedRaw,
			body: "unevaluatedProperties: {type: string}\nunevaluatedItems: {type: integer}\n",
			want: `{"unevaluatedProperties":{"type":"string"},"unevaluatedItems":{"type":"integer"}}`,
			keys: []string{"unevaluatedProperties", "unevaluatedItems"},
		},
		{
			name: "a false unevaluatedProperties is a structural mode, not a keyword to keep",
			read: UnevaluatedRaw, body: "unevaluatedProperties: false\n",
		},
		{name: "no unevaluated keywords", read: UnevaluatedRaw, body: "type: object\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, keys, err := tc.read(schemaFromYAML(t, tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.keys, keys, "the keywords combined, in the requested order")
			if tc.want == "" {
				assert.Nil(t, got, "nothing written yields no object")
				return
			}
			assert.Equal(t, tc.want, string(got), "members keep the requested order")
		})
	}
}

// TestCombinedRaw_AnUnconvertibleMemberFailsTheWhole pins the rule presentMembers
// exists for: an object labelled verbatim that silently omits one of its
// keywords would restate GitHub #144 in miniature, so the combination errors
// instead. The error names the first member that failed, and every keyword
// written is still returned beside it, since the report that stands in for the
// entry is attributed by them (GitHub #534).
func TestCombinedRaw_AnUnconvertibleMemberFailsTheWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		body   string
		read   func(*oas3.Schema) (ir.RawValue, []string, error)
		failed string
		keys   []string
	}{
		{
			name: "if/then/else", read: IfThenElseRaw,
			body:   "if: {type: string}\nthen: {const: .nan}\n",
			failed: "then", keys: []string{"if", "then"},
		},
		{
			name: "contains, with a keyword written after the one that fails", read: ContainsRaw,
			body:   "contains: {const: .nan}\nminContains: 1\n",
			failed: "contains", keys: []string{"contains", "minContains"},
		},
		{
			name: "unevaluated", read: UnevaluatedRaw,
			body:   "unevaluatedProperties: {type: string}\nunevaluatedItems: {const: .nan}\n",
			failed: "unevaluatedItems", keys: []string{"unevaluatedProperties", "unevaluatedItems"},
		},
		{
			name: "two members that fail", read: IfThenElseRaw,
			body:   "if: {const: .nan}\nelse: {const: .inf}\n",
			failed: "if", keys: []string{"if", "else"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, keys, err := tc.read(schemaFromYAML(t, tc.body))
			require.Error(t, err, "the whole combination fails rather than dropping a member")
			assert.Nil(t, got)
			assert.True(t, strings.HasPrefix(err.Error(), tc.failed+": "),
				"the error names the first member that failed: %v", err)
			assert.Equal(t, tc.keys, keys, "every keyword written, whether or not it converted")
		})
	}
}

// TestJSONObject_PropagatesAQuoteFailure covers jsonObject's one failure mode:
// a member key that is not valid UTF-8. Every caller in this package passes a
// fixed JSON Schema keyword as the key, so no real schema reaches this — it is
// exercised directly, the same way rawjson_internal_test.go's
// TestSpliceNumber_RefusesANonJSONNumber exercises a check its own caller can
// no longer fail, guarding against a future member key that is not a
// compile-time constant.
func TestJSONObject_PropagatesAQuoteFailure(t *testing.T) {
	t.Parallel()
	got, err := jsonObject([]rawMember{{key: "\xff", val: ir.RawValue("1")}})

	require.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "invalid UTF-8")
}

// TestValidationOnlyAt_KeepsEveryKeywordItClaims pins the §4.7 family as a set.
// A keyword missing from this list is one the IR neither models nor keeps, which
// is the silent-drop class of defect the family exists to prevent.
func TestValidationOnlyAt_KeepsEveryKeywordItClaims(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\n"+
		"not: {type: string}\n"+
		"if: {type: object}\nthen: {required: [a]}\n"+
		"dependentSchemas: {a: {required: [b]}}\n"+
		"dependentRequired: {a: [b]}\n"+
		"propertyNames: {pattern: '^x'}\n"+
		"contains: {type: string}\nminContains: 1\n"+
		"unevaluatedProperties: {type: string}\n")

	got, diags := validationOnlyAt(s, "/components/schemas/S", sourced(0))

	for _, key := range []string{
		"openapi:not", "openapi:if-then-else", "openapi:dependentSchemas",
		"openapi:dependentRequired", "openapi:propertyNames", "openapi:contains",
		"openapi:unevaluated",
	} {
		entry, ok := got[key]
		require.True(t, ok, "%s must be kept verbatim", key)
		assert.Equal(t, ir.ReasonValidationOnly, entry.Reason, "%s", key)
		assert.NotEmpty(t, entry.Value, "%s must carry bytes", key)
	}
	assert.Len(t, diags, len(got), "each kept keyword is announced exactly once")
}

// TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept pins the error
// route through the combining readers: the keyword yields an error diagnostic
// and no entry, rather than an entry announcing a preservation that failed.
func TestValidationOnlyAt_AnUnconvertibleKeywordIsReportedNotKept(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\nnot: {const: .nan}\ncontains: {const: .nan}\n")

	got, diags := validationOnlyAt(s, "/components/schemas/S", sourced(0))

	assert.NotContains(t, got, "openapi:not")
	assert.NotContains(t, got, "openapi:contains")
	require.GreaterOrEqual(t, len(diags), 2)
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity, "an unconvertible keyword is an error")
	}
}

// TestValidationOnlyAt_AttributesTheEntryAtItsKeyword pins GitHub #522's
// enclosing-object rule from the other side: the kept entry is located at the
// keyword itself and names the overlay that rewrote it, while the
// announcement is located at the schema and stays with the base, because it
// reports on the schema declaring a validation-only keyword rather than on the
// keyword's own text.
func TestValidationOnlyAt_AttributesTheEntryAtItsKeyword(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\nnot: {type: integer}\n")

	got, diags := validationOnlyAt(s, "/components/schemas/S", overlaid("/components/schemas/S/not"))

	entry, ok := got["openapi:not"]
	require.True(t, ok)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/S/not"}, entry.Provenance,
		"the kept entry names the overlay that rewrote the keyword")

	require.Len(t, diags, 1)
	assert.Equal(t, ir.Provenance{Source: 0, Pointer: "/components/schemas/S"}, diags[0].Provenance,
		"the announcement is located at the schema and stays with the base")
}

// TestValidationOnlyAt_AttributesAnUnconvertibleKeywordAtItsPointer is the
// failure half of the test above: a keyword the overlay rewrote into something
// that will not render is reported instead of kept, and the report is located
// at the keyword, so it names the overlay.
func TestValidationOnlyAt_AttributesAnUnconvertibleKeywordAtItsPointer(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\nnot: {const: .nan}\n")

	got, diags := validationOnlyAt(s, "/components/schemas/S", overlaid("/components/schemas/S/not"))

	assert.NotContains(t, got, "openapi:not", "a keyword that will not render is not kept")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/components/schemas/S/not"}, diags[0].Provenance)
}

// TestValidationOnlyAt_AttributesACombinedEntryByItsKeywords pins GitHub #534:
// a combined entry — if/then/else, contains, unevaluated — has no position of
// its own, so it is attributed by asking the Locator about exactly the
// keywords it combines, rather than about the schema alone. A single-keyword
// entry, and the note announcing each entry, ask about nothing but the position
// they are located at.
func TestValidationOnlyAt_AttributesACombinedEntryByItsKeywords(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\n"+
		"not: {type: integer}\n"+
		"if: {required: [name]}\nthen: {required: [tag]}\n"+
		"contains: {type: string}\nminContains: 1\n"+
		"unevaluatedProperties: false\nunevaluatedItems: {type: string}\n")
	var calls []recordedCall

	got, diags := validationOnlyAt(s, "/S", recording(&calls))

	assert.ElementsMatch(t, []recordedCall{
		{pointer: "/S/not"},
		{pointer: "/S", from: []jsontext.Pointer{"/S/if", "/S/then"}},              // not the absent else
		{pointer: "/S", from: []jsontext.Pointer{"/S/contains", "/S/minContains"}}, // not the absent maxContains
		{pointer: "/S", from: []jsontext.Pointer{"/S/unevaluatedItems"}},           // a false unevaluatedProperties is a structural mode
		// and one note per entry, each at the schema
		{pointer: "/S"}, {pointer: "/S"}, {pointer: "/S"}, {pointer: "/S"},
	}, calls)

	// recording answers source 1 only when asked with from, so each source shows
	// which question its record's provenance came from.
	for key, want := range map[string]int{
		"openapi:not": 0, "openapi:if-then-else": 1, "openapi:contains": 1, "openapi:unevaluated": 1,
	} {
		entry, ok := got[key]
		require.True(t, ok, "%s is kept", key)
		assert.Equal(t, want, entry.Provenance.Source, key)
	}
	require.Len(t, diags, 4, "one announcement per entry kept")
	for _, d := range diags {
		assert.Equal(t, 0, d.Provenance.Source, "a note is asked about the schema alone: %s", d.Message)
	}
}

// TestValidationOnlyAt_AttributesAnUnkeptCombinedEntryByItsKeywords is the
// failure half of the test above: a combined entry that will not render is
// reported instead of kept, and the report is attributed as the entry would have
// been, by every keyword written, the one that failed to convert included.
func TestValidationOnlyAt_AttributesAnUnkeptCombinedEntryByItsKeywords(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\n"+
		"if: {required: [name]}\nthen: {const: .nan}\n"+
		"contains: {const: .nan}\nminContains: 1\n")
	var calls []recordedCall

	got, diags := validationOnlyAt(s, "/S", recording(&calls))

	assert.Empty(t, got, "an entry that will not render is not kept")
	assert.ElementsMatch(t, []recordedCall{
		{pointer: "/S", from: []jsontext.Pointer{"/S/if", "/S/then"}},
		{pointer: "/S", from: []jsontext.Pointer{"/S/contains", "/S/minContains"}},
	}, calls)
	require.Len(t, diags, 2)
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity, "%s", d.Message)
		assert.Equal(t, ir.Provenance{Source: 1, Pointer: "/S"}, d.Provenance, "%s", d.Message)
	}
}

// TestValidationOnlyAt_NeedsTheRawNode records a real limit of reading these
// purely: the §4.7 keywords are preserved byte-faithfully from the source YAML,
// so the facet reads the schema's raw root node rather than its parsed fields. A
// schema built in Go carries no root node, so there is nothing to preserve —
// which is the correct answer, not a gap. The parsed-source path is covered by
// the retention suite.
func TestValidationOnlyAt_NeedsTheRawNode(t *testing.T) {
	t.Parallel()
	built := &oas3.Schema{Not: oas3.NewJSONSchemaFromSchema[oas3.Referenceable](&oas3.Schema{})}

	got, diags := validationOnlyAt(built, "/p", sourced(0))

	assert.Nil(t, got, "no raw bytes to keep, so nothing is kept")
	assert.Empty(t, diags, "and nothing is announced for a keyword that was not kept")
}

// TestValidationOnlyAt_DependentRequiredJoinsItsSiblings pins the keyword the
// §4.7 family was missing. dependentRequired is pure validation logic exactly as
// dependentSchemas is, and oas3.Schema has no field for it, which is the whole
// reason it was dropped where its sibling was kept.
func TestValidationOnlyAt_DependentRequiredJoinsItsSiblings(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: object\ndependentRequired:\n  a: [b]\n")

	got, diags := validationOnlyAt(s, "/components/schemas/S", sourced(7))

	entry, ok := got["openapi:dependentRequired"]
	require.True(t, ok, "dependentRequired must be kept verbatim")
	assert.JSONEq(t, `{"a":["b"]}`, string(entry.Value))
	assert.Equal(t, ir.ReasonValidationOnly, entry.Reason)
	assert.Equal(t, jsontext.Pointer("/components/schemas/S/dependentRequired"), entry.Provenance.Pointer)

	require.Len(t, diags, 1, "and is announced exactly once")
	assert.Equal(t, diag.ValidationOnlyKeyword, diags[0].Code)
	assert.Equal(t, 7, diags[0].Provenance.Source)
}

// recordedCall is one (pointer, from) pair a recording Locator was asked
// about.
type recordedCall struct {
	pointer jsontext.Pointer
	from    []jsontext.Pointer
}

// recording is a Locator that appends every call it receives to calls, in call
// order, and answers source 1 when from is non-empty and source 0 otherwise —
// enough to tell a combined entry's call, made with the keywords it holds,
// apart from a single position's, made with none. It exists to let a test
// assert what a reader asked a Locator, rather than only what answer came
// back. An empty from is recorded as nil, so a call made with none compares
// equal to a recordedCall that names none.
func recording(calls *[]recordedCall) Locator {
	return func(pointer jsontext.Pointer, from ...jsontext.Pointer) ir.Provenance {
		call := recordedCall{pointer: pointer}
		source := 0
		if len(from) > 0 {
			call.from = slices.Clone(from)
			source = 1
		}
		*calls = append(*calls, call)
		return ir.Provenance{Source: source, Pointer: pointer}
	}
}
