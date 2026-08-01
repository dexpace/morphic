package openapi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/ir"
)

// unpreservableValue is a value that is legal YAML, decodes, and then cannot be
// marshalled as JSON. It is the shortest reachable trigger for the conversion
// failure: json.Marshal rejects NaN, so any preserved payload containing one
// fails whole.
//
// The other reachable trigger — a mapping whose key is not a string — fails one
// step earlier, in the YAML decode, and is covered directly in
// TestRawFromNode_SeparatesAbsentFromUnconvertible. This one is used for the
// end-to-end fixtures because the loader accepts it in more positions.
const unpreservableValue = ".nan"

// TestUnpreservable_AnnouncementNeverOutrunsTheEntry is the regression test for
// GitHub #144: a diagnostic stating a construct was "kept verbatim under
// Unmodeled" must not be emitted when nothing was written.
//
// It asserts the pairing rather than one site's message, because the defect was
// a property of the idiom rather than of any one caller: every site that
// converts a node and then announces it shared the bug, and four of them still
// had it after the first fix was applied to the fifth.
//
// Each case compiles a document whose preserved payload cannot convert, then
// requires that (a) no degradation diagnostic claims preservation, and (b) the
// unpreservable-construct error is reported in its place.
func TestUnpreservable_AnnouncementNeverOutrunsTheEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec string
		// at is where the failing conversion happened. Requiring it pins that the
		// case reaches the site it is named for: without it a fixture that stopped
		// reaching its site would still satisfy the no-false-claim assertion, by
		// making no claim because it made no lowering.
		at string
	}{
		{
			name: "items after prefixItems",
			spec: componentSpec("    T:\n      type: array\n" +
				"      prefixItems: [{type: string}]\n" +
				"      items: {type: string, x-t: " + unpreservableValue + "}\n"),
			at: "/components/schemas/T/items",
		},
		{
			name: "unmerged allOf branch",
			spec: componentSpec("    M:\n      allOf:\n" +
				"        - {type: string, maxLength: 3, x-t: " + unpreservableValue + "}\n"),
			at: "/components/schemas/M/allOf/0",
		},
		{
			name: "error response with multiple media types",
			spec: pathsSpec("  /x:\n    get:\n      responses:\n" +
				"        \"500\":\n          description: bad\n          content:\n" +
				"            application/json: {schema: {type: string}, example: " + unpreservableValue + "}\n" +
				"            application/xml: {schema: {type: string}}\n"),
			at: "/paths/~1x/get/responses/500/content",
		},
		{
			name: "path-item servers",
			spec: pathsSpec("  /x:\n    servers: [{url: 'https://a', x-t: " + unpreservableValue + "}]\n" +
				"    get:\n      responses: {\"200\": {description: ok}}\n"),
			at: "/paths/~1x/servers",
		},
		{
			name: "residue keyword on a declaration",
			spec: componentSpec("    R:\n      type: object\n" +
				"      default: " + unpreservableValue + "\n" +
				"      properties: {a: {type: string}}\n"),
			at: "/components/schemas/R/default",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, tc.spec)

			assert.True(t, hasDiagCodeAt(diags, diag.UnpreservableConstruct, tc.at),
				"the case must reach the site it is named for: %+v", diags)
			assert.Empty(t, preservationClaims(diags),
				"nothing was written under Unmodeled, so nothing may announce that it was")
		})
	}
}

// TestUnpreservable_SchemaKeywordSites covers the preservation sites reached
// through a schema body rather than through an operation: the §4.7 combination
// keywords, a union kept beside its structural siblings, and the applicators a
// contradictory schema declares that its node has no home for.
//
// They are separated from the table above because they share its property but
// not its shape — several write no diagnostic of their own when they succeed, so
// "no claim was made" is not the assertion that distinguishes them.
func TestUnpreservable_SchemaKeywordSites(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		at   string
	}{
		{
			name: "contains combination",
			body: "      type: array\n      contains: {x-t: " + unpreservableValue + "}\n",
			at:   "/components/schemas/S",
		},
		{
			name: "unevaluated combination",
			body: "      type: object\n      unevaluatedProperties: {x-t: " + unpreservableValue + "}\n",
			at:   "/components/schemas/S",
		},
		{
			// The structural sibling is unconvertible too, so this case has no
			// legitimately preserved construct to account for — the sibling is kept
			// verbatim when it converts, and a claim about it would be sound.
			name: "union beside a structural sibling",
			body: "      items: {type: string, x-t: " + unpreservableValue + "}\n" +
				"      oneOf: [{type: string, x-t: " + unpreservableValue + "}, {type: integer}]\n",
			at: "/components/schemas/S/oneOf",
		},
		{
			name: "applicator with no home on its node",
			body: "      enum: [a]\n      properties: {p: {type: integer, x-t: " + unpreservableValue + "}}\n",
			at:   "/components/schemas/S/properties",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, diags := parseFull(t, componentSpec("    S:\n"+tc.body))
			assert.True(t, hasDiagCodeAt(diags, diag.UnpreservableConstruct, tc.at),
				"the case must reach the site it is named for: %+v", diags)
			assert.Empty(t, preservationClaims(diags),
				"nothing was written under Unmodeled, so nothing may announce that it was")
		})
	}
}

// TestUnpreservable_ReportsTheFailureItself pins the other half: the construct
// that could not be carried is reported, at error severity, rather than lost in
// silence. Kept separate from the pairing test above because a fix that stopped
// announcing and also stopped reporting would satisfy that one.
func TestUnpreservable_ReportsTheFailureItself(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    T:\n      type: array\n" +
		"      prefixItems: [{type: string}]\n" +
		"      items: {type: string, x-t: " + unpreservableValue + "}\n")
	_, diags := parseFull(t, spec)

	d, ok := firstDiagWithCode(diags, diag.UnpreservableConstruct)
	require.True(t, ok, "the unconvertible construct is reported: %+v", diags)
	assert.Equal(t, ir.SeverityError, d.Severity,
		"a construct that reached the IR in no form at all is a losslessness failure, not a degradation")
	assert.Equal(t, "/components/schemas/T/items", d.Provenance.Pointer,
		"the diagnostic locates the construct that was lost, not its owner")
	assert.Contains(t, d.Message, "no form at all")
}

// TestUnpreservable_AbsentConstructIsSilent is the control for the two tests
// above: the same code path must stay silent when there is simply nothing to
// preserve, or the new error would fire on every well-formed document.
func TestUnpreservable_AbsentConstructIsSilent(t *testing.T) {
	t.Parallel()
	_, diags := parseFull(t, componentSpec("    T:\n      type: array\n      items: {type: string}\n"))
	_, ok := firstDiagWithCode(diags, diag.UnpreservableConstruct)
	assert.False(t, ok, "an absent construct is not an unpreservable one: %+v", diags)
}

// TestUnpreservable_CompositeFailsWhole pins the §4.7 combination entries: a
// validation-only object that combines several keywords is presented as the
// verbatim source, so one unconvertible member fails the entry rather than
// silently omitting itself from an object still labelled verbatim.
func TestUnpreservable_CompositeFailsWhole(t *testing.T) {
	t.Parallel()
	spec := componentSpec("    C:\n      type: object\n" +
		"      if: {required: [a]}\n" +
		"      then: {x-t: " + unpreservableValue + "}\n" +
		"      else: {required: [b]}\n")
	doc, diags := parseFull(t, spec)

	_, ok := firstDiagWithCode(diags, diag.UnpreservableConstruct)
	require.True(t, ok, "the unconvertible arm is reported: %+v", diags)
	for key, entry := range typeUnmodeled(t, doc, ids.NamedType(ids.Ptr("components", "schemas", "C"))) {
		assert.NotContains(t, key, "if-then-else",
			"a combination missing an arm is not the verbatim source: %s", entry.Value)
	}
}

// preservationClaims returns every diagnostic message asserting that something
// was kept under Unmodeled. Matching on the claim rather than on a code is
// deliberate: the claim is what the entry has to back, and it is spread across
// several codes and phrasings.
func preservationClaims(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if strings.Contains(d.Message, "under Unmodeled") &&
			d.Code != diag.UnpreservableConstruct {
			out = append(out, d.Message)
		}
	}
	return out
}

// hasDiagCodeAt reports whether diags carries code at exactly pointer.
func hasDiagCodeAt(diags []ir.Diagnostic, code, pointer string) bool {
	for _, d := range diags {
		if d.Code == code && d.Provenance.Pointer == pointer {
			return true
		}
	}
	return false
}

// typeUnmodeled returns the Unmodeled map of one interned type.
func typeUnmodeled(t *testing.T, doc *ir.Document, id ir.TypeID) ir.Unmodeled {
	t.Helper()
	td, ok := doc.Types[id]
	require.True(t, ok, "type %s is interned", id)
	return td.Common().Unmodeled
}

// firstDiagWithCode returns the first diagnostic carrying code.
func firstDiagWithCode(diags []ir.Diagnostic, code string) (ir.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == code {
			return d, true
		}
	}
	return ir.Diagnostic{}, false
}

// TestPreserve_AllReasonsReachable pins that every UnmodeledReason an OpenAPI
// document can provoke is produced by some lowering here, so a consumer
// switching on the enum meets no value this compiler can never emit. There is no
// longer an exception: ReasonOutOfScope was once only ir-design §15's Smithy and
// TypeSpec exclusions, but the JSON Schema resource and dialect keywords land
// under it too (dialectAt, TestDialectKeywords_KeptOutOfScope), so S declares one.
//
// The reach is per reason, not per entry: an entry left at the zero reason is
// invisible here, and is irverify's "ir/empty-unmodeled-reason" to catch across
// the whole corpus rather than this one document's.
func TestPreserve_AllReasonsReachable(t *testing.T) {
	t.Parallel()
	spec := `openapi: 3.1.0
info: {title: T, version: "1"}
components:
  schemas:
    S:
      type: object
      x-vendor: v
      $schema: 'https://json-schema.org/draft/2020-12/schema'
      not: {required: [b]}
      properties: {a: {type: string}}
      oneOf:
        - {required: [a]}
        - {required: [b]}
    T:
      type: array
      prefixItems: [{type: string}]
      items: {type: integer}
paths:
  /p:
    post:
      operationId: p
      requestBody:
        content: {application/json: {schema: {type: string}}}
      responses: {"204": {description: ok}}
`
	doc, svc, diags := lowerServiceSpec(t, spec)
	requireNoErrorDiags(t, diags)

	// S witnesses vendor_extension, validation_only (its constraint-only union
	// joins `not` there) and out_of_scope (its $schema); T's open tuple is the
	// only degraded_lowering witness here, so it must not be folded into another
	// case.
	seen := map[ir.UnmodeledReason]bool{}
	for _, name := range []string{"S", "T"} {
		for _, entry := range doc.Types[componentID(name)].Common().Unmodeled {
			seen[entry.Reason] = true
		}
	}
	// The one no_ir_home site reachable from a minimal document: a requestBody
	// that omits `required`, which the IR has no field for (§14).
	body := firstOp(t, svc).Request
	require.NotNil(t, body, "the operation must own a request payload")
	for _, entry := range body.Unmodeled {
		seen[entry.Reason] = true
	}

	for _, want := range []ir.UnmodeledReason{
		ir.ReasonVendorExtension, ir.ReasonValidationOnly,
		ir.ReasonDegradedLowering, ir.ReasonNoIRHome, ir.ReasonOutOfScope,
	} {
		assert.True(t, seen[want], "no entry carries reason %q", want)
	}
}
