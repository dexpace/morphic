package e2e_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

func assertParamStyles(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "search")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	byParam := map[string]ir.HTTPParamBinding{}
	for _, pb := range op.Bindings.HTTP[0].ParamBindings {
		byParam[pb.WireName] = pb
	}
	q := byParam["q"]
	assert.Equal(t, "form", q.Style, "query default style is form")
	require.NotNil(t, q.Explode)
	assert.True(t, *q.Explode, "form default explodes")
	assert.Equal(t, "deepObject", byParam["filter"].Style)
	assert.Equal(t, "application/json", byParam["payload"].ContentType, "content-style param records its media type")
	assert.True(t, byParam["path"].AllowReserved,
		"reserved characters passing through unescaped is a wire fact, not a style")
	assert.False(t, q.AllowReserved, "and the default is to escape them")

	// "path" is a query parameter in this 3.1 document, the one location the
	// keyword belongs to before 3.2 — so a declaration of it, which the
	// assertion above pins, is not the off-query case the warning reports.
	assert.Zero(t, openapitest.CountDiagsAt(diags, diag.InapplicableLocationKeyword, ir.SeverityWarning),
		"a 3.1 query declaration of allowReserved stays silent")

	assertAllowEmptyValueLowers(t, op)

	// The path item's shared parameter merges into both of its operations and
	// interns its schema once, at the path item's own pointer (issue #36).
	clearSearch, ok := opByName(doc, "clearSearch")
	require.True(t, ok, "the path item's second operation is registered")
	searchReqID, ok := openapitest.ParamBySource(op, "requestId")
	require.True(t, ok, "search inherits the path-item-level parameter")
	clearReqID, ok := openapitest.ParamBySource(clearSearch, "requestId")
	require.True(t, ok, "clearSearch inherits the same path-item-level parameter")
	assert.Equal(t, searchReqID.Type.Target, clearReqID.Type.Target,
		"both operations resolve the shared path-item parameter to the same interned schema")
	assert.Equal(t, ir.TypeID("t/anon/paths/~1search/parameters/0/schema"), searchReqID.Type.Target,
		"the shared schema is hoisted at the path item's own pointer, not a per-operation one")
}

// assertAllowEmptyValueLowers covers the last serialization flag to reach
// ir.HTTPParamBinding. Style, explode, allowReserved and the content-style media
// type land on the binding above; allowEmptyValue used to be read nowhere at
// all, so a document declaring it produced IR indistinguishable from one that
// did not (GitHub #39), and then only verbatim on the logical Parameter.
func assertAllowEmptyValueLowers(t *testing.T, op ir.Operation) {
	t.Helper()
	bare, ok := openapitest.ParamBySource(op, "bare")
	require.True(t, ok, "the allowEmptyValue parameter still lowers")
	assert.NotContains(t, bare.Unmodeled, "openapi:allowEmptyValue", "and keeps nothing verbatim")
	require.Len(t, op.Bindings.HTTP, 1)
	for _, b := range op.Bindings.HTTP[0].ParamBindings {
		assert.Equal(t, b.Param == bare.ID, b.AllowEmptyValue,
			"%s: only the parameter declaring it allows an empty value", b.WireName)
	}
}

// paramSlot is what OpenAPI identifies a parameter by: its name and location.
// Two parameters may legally share a name at different locations, so a map
// keyed on the name alone silently keeps one of such a pair and compares the
// other against nothing.
type paramSlot struct {
	Name     string
	Location ir.HTTPLocation
}

// paramWire is what a parameter's location-dependent serialization resolves to:
// the style it serializes with, and whether it explodes. The two travel together
// because OpenAPI settles them together — the location picks the style when none
// is written, and the style picks explode.
type paramWire struct {
	Style   string
	Explode bool
}

// paramStylePair is one legal (in, style) combination of the Parameter object.
type paramStylePair struct {
	location ir.HTTPLocation
	style    string
}

// param names the fixture parameter declaring this pair with one explode
// spelling: "<location><Style>" and the suffix.
//
// It is total rather than slicing p.style directly: the table's own comment
// points out that querystring takes no style, which is an invitation to add a
// styleless pair, and a panic inside a name builder says nothing about which
// pair caused it. TestParamStyleTable_MatchesTheSpecification is what rejects
// one, with the pair named.
func (p paramStylePair) param(suffix string) string {
	return string(p.location) + upperFirst(p.style) + suffix
}

// upperFirst capitalizes the first letter of an ASCII style name.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// paramStyleLegalPairs is OpenAPI's (in, style) table, read off the
// specification rather than off the fixture: path takes simple|label|matrix,
// query form|spaceDelimited|pipeDelimited|deepObject, header simple, cookie
// form|cookie. querystring is a fifth location that takes no style at all, so
// it has no pair here and assertQuerystringParam covers it instead.
func paramStyleLegalPairs() []paramStylePair {
	return []paramStylePair{
		{ir.HTTPLocationPath, "simple"},
		{ir.HTTPLocationPath, "label"},
		{ir.HTTPLocationPath, "matrix"},
		{ir.HTTPLocationQuery, "form"},
		{ir.HTTPLocationQuery, "spaceDelimited"},
		{ir.HTTPLocationQuery, "pipeDelimited"},
		{ir.HTTPLocationQuery, "deepObject"},
		{ir.HTTPLocationHeader, "simple"},
		{ir.HTTPLocationCookie, "form"},
		{ir.HTTPLocationCookie, "cookie"},
	}
}

// paramStyleDefaultStyles is the other half of the same table: the style each
// location falls back to when a parameter omits `style` entirely.
func paramStyleDefaultStyles() []paramStylePair {
	return []paramStylePair{
		{ir.HTTPLocationPath, "simple"},
		{ir.HTTPLocationQuery, "form"},
		{ir.HTTPLocationHeader, "simple"},
		{ir.HTTPLocationCookie, "form"},
	}
}

// paramStyleExplodeDefault is what `explode` means when omitted: true under
// form and under 3.2's cookie style, false under every other style a location
// pairs with.
func paramStyleExplodeDefault(style string) bool {
	return style == "form" || style == "cookie"
}

// paramStyleMatrixWant generates what param-style-matrix.yaml must resolve to
// from the two tables above, rather than restating it beside them. A literal
// expectation and the fixture are two copies of one claim, so deleting a row
// from both leaves nothing to disagree, and counts over a deduplicated set can
// still pass a shrunk fixture when another parameter resolves the same way.
// Generating moves the question up: the table and the fixture can still shrink
// together. TestParamStyleTable_MatchesTheSpecification holds the table to a
// count that is OpenAPI's rather than this package's.
func paramStyleMatrixWant() map[paramSlot]paramWire {
	want := map[paramSlot]paramWire{}
	for _, p := range paramStyleLegalPairs() {
		for _, e := range []struct {
			suffix  string
			explode bool
		}{
			{"Explode", true},
			{"NoExplode", false},
			{"DefaultExplode", paramStyleExplodeDefault(p.style)},
		} {
			want[paramSlot{p.param(e.suffix), p.location}] = paramWire{p.style, e.explode}
		}
	}
	for _, d := range paramStyleDefaultStyles() {
		want[paramSlot{string(d.location) + "Defaulted", d.location}] =
			paramWire{d.style, paramStyleExplodeDefault(d.style)}
	}
	return want
}

// TestParamStyleTable_MatchesTheSpecification anchors the (in, style) table to
// OpenAPI's own, which a generated expectation cannot do for itself. Deleting a
// pair and the parameters it generated, then regenerating the golden, leaves
// every check agreeing with the smaller world; only the specification is outside
// that loop. 3.2 tabulates the legal combinations and says "Combinations not
// represented in this table are not permitted", so this count is OpenAPI's and
// changing it is a claim about the format.
//
// It also holds the two halves of the table to each other: a location's default
// style must be a style that location legally takes.
func TestParamStyleTable_MatchesTheSpecification(t *testing.T) {
	t.Parallel()
	pairs := paramStyleLegalPairs()
	require.Len(t, pairs, 10,
		"3.2 permits ten (in, style) pairs; a different number here is a claim about OpenAPI, not about the fixture")

	legal := make(map[paramStylePair]bool, len(pairs))
	for _, p := range pairs {
		assert.NotEmpty(t, p.style,
			"%s: a pair names a style, and a location that takes none belongs in neither table", p.location)
		assert.NotContains(t, legal, p, "the (%s, %s) pair is listed twice", p.location, p.style)
		legal[p] = true
	}

	defaults := paramStyleDefaultStyles()
	require.Len(t, defaults, 4, "each location that takes a style at all defaults exactly one")
	locations := make(map[ir.HTTPLocation]bool, len(defaults))
	for _, d := range defaults {
		assert.NotContains(t, locations, d.location, "%s is given two default styles", d.location)
		locations[d.location] = true
		assert.Contains(t, legal, d,
			"%s defaults to style %q, which is not a pair the table says that location takes",
			d.location, d.style)
	}
}

// assertParamStyleMatrix pins the whole of OpenAPI's location-dependent
// serialization table at once, against a generated expectation rather than a
// spot check per location.
//
// The table used to be readable for a whole location without any golden
// noticing: deleting cookie from the arm that defaults it to form left the
// entire suite green, because no committed spec declared a cookie parameter.
// Comparing the resolved (style, explode) of every parameter closes that by
// construction — a location that loses its arm changes rows here.
func assertParamStyleMatrix(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "styleMatrix")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)

	got := make(map[paramSlot]paramWire, len(op.Bindings.HTTP[0].ParamBindings))
	for _, pb := range op.Bindings.HTTP[0].ParamBindings {
		require.NotNil(t, pb.Explode, "%s: explode resolves to a value, never to nothing", pb.WireName)
		slot := paramSlot{Name: pb.WireName, Location: pb.Location}
		require.NotContains(t, got, slot, "two parameter bindings share the slot %+v", slot)
		got[slot] = paramWire{Style: pb.Style, Explode: *pb.Explode}
	}
	if diff := cmp.Diff(paramStyleMatrixWant(), got); diff != "" {
		t.Errorf("resolved parameter serialization (-want +got):\n%s", diff)
	}

	assertQuerystringParam(t, doc)
}

// assertQuerystringParam covers the 3.2 location that binds the whole query
// string. It may declare neither style nor schema, so the media type its
// one-entry content map names is the whole of its stated serialization.
//
// It has an operation to itself because 3.2 forbids a querystring parameter from
// sharing one, or its path item, with any `in: query` parameter; the test
// asserts the operation binds exactly one.
//
// It carries neither style nor explode: 3.2 gives the location no style, and
// explode qualifies a style there is none of (GitHub #334). Both are pinned so a
// stamped form/true default cannot return.
func assertQuerystringParam(t *testing.T, doc *ir.Document) {
	t.Helper()
	op, ok := opByName(doc, "querystringOnly")
	require.True(t, ok, "the querystring parameter's own operation is registered")
	require.Len(t, op.Bindings.HTTP, 1)
	require.Len(t, op.Bindings.HTTP[0].ParamBindings, 1,
		"the querystring location shares its operation with no other parameter")

	binding := op.Bindings.HTTP[0].ParamBindings[0]
	require.Equal(t, ir.HTTPLocationQuerystring, binding.Location)
	require.Equal(t, "querystringWhole", binding.WireName, "the querystring parameter binds")
	assert.Equal(t, "application/x-www-form-urlencoded", binding.ContentType,
		"its media type is where its serialization is actually stated")
	assert.Empty(t, binding.Style, "style is not a legal keyword at in: querystring")
	assert.Nil(t, binding.Explode, "and explode qualifies a style there is none of")
}

// assertParamQuerystring pins the 3.2 querystring location, where the whole
// query string binds from the parameter's content: the binding carries that
// media type and neither style nor explode, which the location forbids (GitHub
// #334).
//
// The third operation holds what param-style-matrix does not: only the default
// is suppressed, so a declared explode survives at a location that takes no
// style. param-style-matrix declares neither keyword there, so reverting the
// early return that dropped a declared explode leaves it green. This case also
// pins the one diag.InvalidLocationKeyword this fixture raises; see that code's
// GoDoc for why it is a warning.
func assertParamQuerystring(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	report, ok := opByName(doc, "runReport")
	require.True(t, ok)
	require.Len(t, report.Bindings.HTTP, 1)
	require.Len(t, report.Bindings.HTTP[0].ParamBindings, 1)
	qs := report.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, ir.HTTPLocationQuerystring, qs.Location)
	assert.Equal(t, "application/x-www-form-urlencoded", qs.ContentType,
		"the media type is the whole of a querystring binding's declared serialization")
	assert.Empty(t, qs.Style, "style is not a legal keyword at in: querystring")
	assert.Nil(t, qs.Explode, "and explode qualifies a style there is none of")

	summary, ok := opByName(doc, "summarize")
	require.True(t, ok)
	require.Len(t, summary.Bindings.HTTP, 1)
	require.Len(t, summary.Bindings.HTTP[0].ParamBindings, 1)
	q := summary.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, ir.HTTPLocationQuery, q.Location)
	assert.Equal(t, "form", q.Style, "a query parameter still takes its own location's default")
	require.NotNil(t, q.Explode)
	assert.True(t, *q.Explode)

	raw, ok := opByName(doc, "rawReport")
	require.True(t, ok)
	require.Len(t, raw.Bindings.HTTP, 1)
	require.Len(t, raw.Bindings.HTTP[0].ParamBindings, 1)
	declared := raw.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, ir.HTTPLocationQuerystring, declared.Location)
	assert.Empty(t, declared.Style, "no style is invented beside a declared explode either")
	require.NotNil(t, declared.Explode, "but the explode the document declares is not dropped")
	assert.False(t, *declared.Explode)

	assert.Equal(t,
		"parameter field explode is not allowed for in=querystring; lowered as declared",
		openapitest.DiagMessageAt(t, diags, diag.InvalidLocationKeyword, ir.SeverityWarning,
			"/paths/~1reports~1raw/get/parameters/0/explode"))
}

// assertQuerystringForbiddenKeywords pins the construct assertParamQuerystring's
// rawReport operation cannot see on its own: explode and allowReserved reported
// at their own pointers rather than only one of the two. See
// diag.InvalidLocationKeyword's GoDoc for why the parser catches neither and why
// this compiler's report is a warning. Both values still lower as declared:
// dropping content the document states is an emitter's call, not a compiler's.
func assertQuerystringForbiddenKeywords(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	explodeOp, ok := opByName(doc, "explodeOnly")
	require.True(t, ok)
	require.Len(t, explodeOp.Bindings.HTTP, 1)
	require.Len(t, explodeOp.Bindings.HTTP[0].ParamBindings, 1)
	explodeBinding := explodeOp.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, ir.HTTPLocationQuerystring, explodeBinding.Location)
	require.NotNil(t, explodeBinding.Explode, "the declared explode is not dropped")
	assert.False(t, *explodeBinding.Explode)
	assert.Equal(t,
		"parameter field explode is not allowed for in=querystring; lowered as declared",
		openapitest.DiagMessageAt(t, diags, diag.InvalidLocationKeyword, ir.SeverityWarning,
			"/paths/~1a/get/parameters/0/explode"))

	reservedOp, ok := opByName(doc, "reservedOnly")
	require.True(t, ok)
	require.Len(t, reservedOp.Bindings.HTTP, 1)
	require.Len(t, reservedOp.Bindings.HTTP[0].ParamBindings, 1)
	reservedBinding := reservedOp.Bindings.HTTP[0].ParamBindings[0]
	assert.Equal(t, ir.HTTPLocationQuerystring, reservedBinding.Location)
	assert.True(t, reservedBinding.AllowReserved, "the declared allowReserved is not dropped")
	assert.Equal(t,
		"parameter field allowReserved is not allowed for in=querystring; lowered as declared",
		openapitest.DiagMessageAt(t, diags, diag.InvalidLocationKeyword, ir.SeverityWarning,
			"/paths/~1b/get/parameters/0/allowReserved"))
}

// assertParamAllowReservedLocations pins diag.InapplicableLocationKeyword
// through the whole compile: OpenAPI 3.1 applies allowReserved to in: query
// alone, so a path, header or cookie declaration is reported at the keyword's
// own pointer, a declared false among them, while a query declaration stays
// silent. Every declared value still lowers as declared.
func assertParamAllowReservedLocations(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "report")
	require.True(t, ok)
	require.Len(t, op.Bindings.HTTP, 1)
	byParam := map[string]ir.HTTPParamBinding{}
	for _, pb := range op.Bindings.HTTP[0].ParamBindings {
		byParam[pb.WireName] = pb
	}
	require.Len(t, byParam, 4, "every declared parameter binds exactly once")

	assert.True(t, byParam["id"].AllowReserved, "a path declaration still lowers as declared")
	assert.True(t, byParam["X-Trace"].AllowReserved, "a header declaration still lowers as declared")
	assert.False(t, byParam["session"].AllowReserved, "a declared false is kept as false")
	assert.True(t, byParam["q"].AllowReserved, "a query declaration lowers as declared")

	const rule = " in OpenAPI 3.1, which applies it to in=query alone; lowered as declared"
	want := map[string]string{
		"/paths/~1reports~1{id}/get/parameters/0/allowReserved": "parameter field allowReserved has no effect for in=path" + rule,
		"/paths/~1reports~1{id}/get/parameters/1/allowReserved": "parameter field allowReserved has no effect for in=header" + rule,
		"/paths/~1reports~1{id}/get/parameters/2/allowReserved": "parameter field allowReserved has no effect for in=cookie" + rule,
	}
	got := map[string]string{}
	for _, d := range diags {
		if d.Code == diag.InapplicableLocationKeyword {
			assert.Equal(t, ir.SeverityWarning, d.Severity)
			assert.NotContains(t, got, string(d.Provenance.Pointer), "one declaration, one report")
			got[string(d.Provenance.Pointer)] = d.Message
		}
	}
	assert.Equal(t, want, got)
}

// assertParamRefInheritance pins the rule
// docs/compilers/openapi.md#ref-use-site at a parameter whose schema is a $ref:
// docs, deprecation and default come from the referent when the use site is
// silent, and from the use site when it is not. Constraints inherit at neither
// carrier, so the identical property is asserted beside it (GitHub #131). The
// bound the use site declares beside the $ref is asserted at both carriers
// against the referent's own: use-site precedence would publish 100 as the
// whole truth while the document enforces 64 (§12.2, GitHub #428).
func assertParamRefInheritance(t *testing.T, doc *ir.Document, _ []ir.Diagnostic) {
	op, ok := opByName(doc, "listItems")
	require.True(t, ok)
	cursor, ok := openapitest.ParamBySource(op, "cursor")
	require.True(t, ok)
	assert.Equal(t, ir.Docs{Summary: "Cursor", Description: "Opaque pagination cursor."}, cursor.Docs)
	assert.NotNil(t, cursor.Deprecation)
	require.NotNil(t, cursor.Default)
	assert.Equal(t, "0", cursor.Default.Str)
	assert.Nil(t, cursor.Constraints, "the referent's bound stays on the referent")

	override, ok := openapitest.ParamBySource(op, "override")
	require.True(t, ok)
	assert.Equal(t, "Cursor for this endpoint only.", override.Docs.Description,
		"a description beside the $ref wins over the referent's")
	assert.Equal(t, "Cursor", override.Docs.Summary,
		"...and a keyword the use site is silent about still inherits")
	require.NotNil(t, override.Default)
	assert.Equal(t, "9", override.Default.Str)
	require.NotNil(t, override.Constraints, "a bound beside the $ref lands on the carrier")
	require.NotNil(t, override.Constraints.MaxLength)
	assert.Equal(t, int64(100), *override.Constraints.MaxLength,
		"and it is the use site's own, not narrowed against the referent's here")

	holder, ok := doc.Types[componentID("Holder")].(*ir.Model)
	require.True(t, ok)
	prop, ok := propByWire(holder, "cursor")
	require.True(t, ok)
	assert.Equal(t, cursor.Docs, prop.Docs, "the property inherits exactly what the parameter does")
	assert.NotNil(t, prop.Deprecation)
	require.NotNil(t, prop.Default)
	assert.Equal(t, *cursor.Default, *prop.Default)
	assert.Nil(t, prop.Constraints, "neither carrier inherits the referent's constraints")

	overrideProp, ok := propByWire(holder, "override")
	require.True(t, ok)
	assert.Equal(t, override.Constraints, overrideProp.Constraints,
		"and a property keeps its own bound exactly as the parameter does")

	decl, ok := doc.Types[componentID("Cursor")].(*ir.Scalar)
	require.True(t, ok)
	require.NotNil(t, decl.Constraints)
	require.NotNil(t, decl.Constraints.MaxLength)
	assert.Equal(t, int64(64), *decl.Constraints.MaxLength,
		"a consumer that wants the bound reads it off the referent, and conjoins "+
			"it with the use site's: both are in force, and 64 is the narrower")
}

// assertHeaderContentSchema pins that both spellings of a header's type lower
// alike. The content spelling used to reach lowerHeader with no schema at all —
// the header became t/prim/any and its constraints and xml hints went with it,
// without a diagnostic (GitHub #139).
//
// The two headers declare the same type deliberately: comparing them against
// each other, rather than against a written-out expectation, is what makes the
// assertion about the spellings agreeing rather than about one of them.
func assertHeaderContentSchema(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "getReport")
	require.True(t, ok)
	require.Len(t, op.Responses, 1)
	headers := op.Responses[0].Headers
	require.Len(t, headers, 4)

	bySchema, ok := headerByWire(headers, "X-Report-Schema")
	require.True(t, ok)
	byContent, ok := headerByWire(headers, "X-Report-Content")
	require.True(t, ok)

	assert.Equal(t, bySchema.Type, byContent.Type,
		"a content-style header resolves the same type as the schema spelling")
	assert.NotEqual(t, ir.TypeID("t/prim/any"), byContent.Type.Target,
		"the type is resolved rather than degraded to the top type")
	assert.Equal(t, bySchema.Constraints, byContent.Constraints,
		"the constraints written under content are not dropped with the schema")
	require.NotNil(t, byContent.XML)
	assert.Equal(t, "ReportContent", byContent.XML.Name,
		"the xml hints written under content reach the header")

	require.NotNil(t, byContent.Encoding)
	assert.Equal(t, "application/xml", byContent.Encoding.MediaType,
		"the media type serializing the header's value is kept, which the schema spelling has none of")
	assert.Nil(t, bySchema.Encoding, "the schema spelling names no media type")

	byRef, ok := headerByWire(headers, "X-Report-Ref")
	require.True(t, ok)
	assert.Equal(t, componentID("ReportID"), byRef.Type.Target,
		"a $ref under content resolves to the named component, not to an anonymous copy")

	assertHeaderStyleAndExplodeLower(t, headers)
	assertNoErrorDiags(t, diags)
}

// assertHeaderStyleAndExplodeLower covers the header keywords that say how its
// value goes on the wire. A header's explode decides how an object value is
// joined, so dropping it left the IR unable to say how the header serializes —
// and it was dropped without a word (GitHub #39). They lower onto Header.Style
// and Explode, as declared.
func assertHeaderStyleAndExplodeLower(t *testing.T, headers []ir.Header) {
	t.Helper()
	list, ok := headerByWire(headers, "X-Report-List")
	require.True(t, ok)
	assert.Equal(t, "simple", list.Style)
	require.NotNil(t, list.Explode)
	assert.True(t, *list.Explode)
	assert.NotContains(t, list.Unmodeled, "openapi:style", "neither is kept verbatim")
	assert.NotContains(t, list.Unmodeled, "openapi:explode")

	plain, ok := headerByWire(headers, "X-Report-Schema")
	require.True(t, ok)
	assert.Empty(t, plain.Style, "a header that declares neither keyword sets neither")
	assert.Nil(t, plain.Explode)
}

// assertParamContentFields pins GitHub #611: electing a parameter's or header's
// `content` spelling reads the whole Media Type Object rather than only its
// schema, so the object's examples reach the carrier and what the carrier has no
// home for is kept verbatim under the content entry's own scope. The schema
// spelling beside them elects no media type and records none of it.
func assertParamContentFields(t *testing.T, doc *ir.Document, diags []ir.Diagnostic) {
	op, ok := opByName(doc, "search")
	require.True(t, ok)

	filter, ok := openapitest.ParamBySource(op, "filter")
	require.True(t, ok)
	require.Len(t, filter.Examples, 1, "the media type's examples reach the parameter")
	assert.Equal(t, "one", filter.Examples[0].Name)
	assert.Equal(t, "One kind", filter.Examples[0].Summary)

	const scope = "openapi:content/application~1json/"
	assert.Equal(t, ir.ReasonVendorExtension, filter.Unmodeled[scope+"x-note"].Reason)
	assert.Equal(t, ir.ReasonNoIRHome, filter.Unmodeled[scope+"itemSchema"].Reason,
		"a parser-modelled field with no ir.Parameter home is kept rather than dropped")

	plain, ok := openapitest.ParamBySource(op, "plain")
	require.True(t, ok)
	assert.Empty(t, plain.Examples, "the schema spelling elects no media type to take examples from")
	assert.Empty(t, plain.Unmodeled, "nor any content-scoped entry")

	require.Len(t, op.Responses, 1)
	require.Len(t, op.Responses[0].Headers, 1)
	header := op.Responses[0].Headers[0]
	require.Len(t, header.Examples, 2, "the header's own example and its media type's are both kept")
	assert.Empty(t, header.Examples[0].Name, "its own singular example comes first")
	assert.Equal(t, "hit", header.Examples[1].Name)
	assert.Equal(t, ir.ReasonVendorExtension, header.Unmodeled[scope+"x-hdr"].Reason)

	openapitest.AssertInfoDiagAt(t, diags,
		"/paths/~1search/get/parameters/0/content/application~1json/itemSchema")
}
