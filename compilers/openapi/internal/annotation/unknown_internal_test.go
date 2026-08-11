package annotation

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// TestUnknownKeywordsIn_KeepsWhatTheModelDoesNotName is the census at its own
// subject: a keyword with no field in the schema model reaches the IR under its
// own key, located at itself, and is announced at info.
func TestUnknownKeywordsIn_KeepsWhatTheModelDoesNotName(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-ray: kept\nnotAKeyword: 7\n")

	var got ir.Unmodeled
	diags := UnknownKeywordsIn(&got, s, "/components/schemas/A", 3)

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
	diags := UnknownKeywordsIn(&got, s, "/components/schemas/A", 0)

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

	diags := UnknownKeywordsIn(&got, s, "/A", 0)

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
	diags := UnknownKeywordsIn(&got, s, "/A", 0)

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
	obj := fakeObject{core: &fakeCore{keys: reported}, root: parsedMapping(t, "zeta: 1\nalpha: 2\n")}

	var got ir.Unmodeled
	diags := UnknownKeysUnder(&got, obj, 1, "/info/contact", "info/contact")

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
	obj := fakeObject{core: &fakeCore{keys: keys}, root: parsedMapping(t, body.String())}

	var got ir.Unmodeled
	diags := UnknownKeysIn(&got, obj, 0, "/x")

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
	obj := fakeObject{core: &fakeCore{keys: []string{"absent"}}, root: parsedMapping(t, "present: 1\n")}

	var got ir.Unmodeled
	diags := UnknownKeysIn(&got, obj, 2, "/x")

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
		root: parsedMapping(t, "info/contact/slack: fromRoot\n"),
	}
	contact := fakeObject{
		core: &fakeCore{keys: []string{"slack"}},
		root: parsedMapping(t, "slack: fromContact\n"),
	}

	var got ir.Unmodeled
	diags := UnknownKeysUnder(&got, root, 0, "", "")
	diags = append(diags, UnknownKeysUnder(&got, contact, 0, "/info/contact", "info/contact")...)

	assert.Equal(t, ir.RawValue(`"fromRoot"`), got["openapi:info~1contact~1slack"].Value,
		"the root's key is one segment, escaped")
	assert.Equal(t, ir.RawValue(`"fromContact"`), got["openapi:info/contact/slack"].Value,
		"the scoped entry is the contact object's, and the root cannot spell it")
	assert.Len(t, diags, 2, "two keys survive, so two are announced")
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

			diags := UnknownKeysIn(&got, tc.model, 0, "/x")

			assert.Nil(t, got)
			assert.Empty(t, diags)
		})
	}
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

// parsedMapping parses body into the mapping node the census reads values from.
func parsedMapping(t *testing.T, body string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(body), &doc))
	return &doc
}
