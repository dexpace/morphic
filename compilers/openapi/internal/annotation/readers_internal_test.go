package annotation

import (
	"strings"
	"testing"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// jsonSchemaFromYAML unmarshals body as a JSONSchema, keeping the reference and
// boolean forms a bare *oas3.Schema cannot represent.
func jsonSchemaFromYAML(t *testing.T, body string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	js := &oas3.JSONSchema[oas3.Referenceable]{}
	valErrs, err := marshaller.Unmarshal(t.Context(), strings.NewReader(body), js)
	require.NoError(t, err)
	require.Empty(t, valErrs, "the fixture parses cleanly")
	return js
}

// resolvedProperty parses body as a schema and resolves property p against it,
// which is what gives the position a Referent to read through.
func resolvedProperty(t *testing.T, body, p string) *oas3.JSONSchema[oas3.Referenceable] {
	t.Helper()
	root := jsonSchemaFromYAML(t, body)
	prop, ok := root.GetSchema().GetProperties().Get(p)
	require.True(t, ok, "the fixture declares property %q", p)
	valErrs, err := prop.Resolve(t.Context(), oas3.ResolveOptions{
		RootDocument: root, TargetLocation: "spec.yaml", DisableExternalRefs: true,
	})
	require.NoError(t, err)
	require.Empty(t, valErrs)
	return prop
}

// TestAt_ClassifiesEveryPositionShape pins the classification every annotation
// read starts from. A declaration misread as a reference would fall back to a
// referent that does not describe it, and a reference misread as a declaration
// would lose the fallback entirely.
func TestAt_ClassifiesEveryPositionShape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		body         string
		wantKind     Kind
		wantNode     bool
		wantReferent bool
	}{
		{name: "a written body is a declaration", body: "type: string\n", wantKind: Declaration, wantNode: true},
		{name: "a boolean schema has no body", body: "true\n", wantKind: Declaration},
		{
			name: "an unresolved $ref is a reference with no referent",
			body: "{$ref: '#/$defs/Missing'}\n", wantKind: Reference, wantNode: true,
		},
		{
			name: "an empty $ref resolves nowhere, so it is a declaration",
			body: "{$ref: ''}\n", wantKind: Declaration, wantNode: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := At(jsonSchemaFromYAML(t, tc.body))
			assert.Equal(t, tc.wantKind, got.Kind)
			assert.Equal(t, tc.wantNode, got.Node != nil, "Node presence")
			assert.Equal(t, tc.wantReferent, got.Referent != nil, "Referent presence")
		})
	}

	t.Run("a nil position is an empty declaration", func(t *testing.T) {
		t.Parallel()
		got := At(nil)
		assert.Equal(t, Declaration, got.Kind)
		assert.Nil(t, got.Node)
		assert.Nil(t, got.Referent)
	})
}

// TestAt_ResolvedReferenceCarriesOneHop pins the half of At that needs real
// resolution: a resolved $ref exposes the declaration it points at, and the
// body written beside the $ref stays on Node rather than being replaced by it.
func TestAt_ResolvedReferenceCarriesOneHop(t *testing.T) {
	t.Parallel()
	prop := resolvedProperty(t,
		"$defs:\n  Target: {type: string, title: fromTarget}\n"+
			"properties:\n  p: {$ref: '#/$defs/Target', description: fromSite}\n", "p")

	got := At(prop)

	require.Equal(t, Reference, got.Kind)
	require.NotNil(t, got.Referent, "a resolved $ref exposes its one-hop declaration")
	assert.Equal(t, "fromTarget", got.Referent.GetTitle())
	require.NotNil(t, got.Node, "the body written beside the $ref is the site's own")
	assert.Equal(t, "fromSite", got.Node.GetDescription())
}

// TestDeclaredSchema_NilWithoutResolution pins that an unresolved $ref yields no
// declaration rather than a zero one. A non-nil answer here would have At report
// a Referent that carries nothing, which is indistinguishable from a target that
// genuinely declares nothing.
func TestDeclaredSchema_NilWithoutResolution(t *testing.T) {
	t.Parallel()
	assert.Nil(t, DeclaredSchema(jsonSchemaFromYAML(t, "{$ref: '#/$defs/Missing'}\n")))
	assert.Nil(t, DeclaredSchema(jsonSchemaFromYAML(t, "type: string\n")))
}

// TestSchemaOf_ReturnsOnlyAWrittenBody pins the one nil-safe reader every site
// goes through: a position with no body written at it must report none, because
// a boolean schema and a nil position both admit no annotations at all.
func TestSchemaOf_ReturnsOnlyAWrittenBody(t *testing.T) {
	t.Parallel()
	assert.Nil(t, SchemaOf(nil), "a nil position writes no body")
	assert.Nil(t, SchemaOf(jsonSchemaFromYAML(t, "true\n")), "a boolean schema writes no body")
	assert.Nil(t, SchemaOf(jsonSchemaFromYAML(t, "false\n")), "including the false one")

	s := SchemaOf(jsonSchemaFromYAML(t, "type: string\ntitle: T\n"))
	require.NotNil(t, s)
	assert.Equal(t, "T", s.GetTitle())
}

// TestEffectiveVisibility_MapsTheFlagsToLifecycles pins the readOnly/writeOnly
// mapping of ir-design §5.2, the precedence a site holds over its referent, and
// the answer for the pairing that admits nothing.
//
// Precedence is per flag: a site that writes readOnly settles readOnly for the
// position and says nothing about writeOnly, which therefore still resolves
// from the referent — the uniform §14 merge every other annotation here already
// follows. So a site's readOnly does not cancel a referent's writeOnly; the two
// are both in force, they admit disjoint lifecycle sets, and the position is
// left visible in none. That case read as plain readOnly until GitHub #276,
// when the flag arriving second was dropped without a word.
func TestEffectiveVisibility_MapsTheFlagsToLifecycles(t *testing.T) {
	t.Parallel()
	read := ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleRead, ir.LifecycleDelete, ir.LifecycleQuery}}
	write := ir.Visibility{Only: []ir.Lifecycle{ir.LifecycleCreate, ir.LifecycleUpdate}}
	none := ir.Visibility{None: true}

	tests := []struct {
		name         string
		ref, tgt     string
		want         ir.Visibility
		wantDisjoint bool
	}{
		{name: "readOnly at the site", ref: "readOnly: true\n", want: read},
		{name: "writeOnly at the site", ref: "writeOnly: true\n", want: write},
		{name: "readOnly inherited from the referent", ref: "type: string\n", tgt: "readOnly: true\n", want: read},
		{name: "writeOnly inherited from the referent", ref: "type: string\n", tgt: "writeOnly: true\n", want: write},
		{name: "the site wins over the referent for the flag it writes", ref: "readOnly: true\n", tgt: "readOnly: false\n", want: read},
		{name: "a false flag is still the site's answer", ref: "readOnly: false\n", tgt: "readOnly: true\n"},
		{name: "both flags on one schema", ref: "readOnly: true\nwriteOnly: true\n", want: none, wantDisjoint: true},
		{name: "both flags on the referent", ref: "type: string\n", tgt: "readOnly: true\nwriteOnly: true\n", want: none, wantDisjoint: true},
		{name: "the site's readOnly leaves the referent's writeOnly in force", ref: "readOnly: true\n", tgt: "writeOnly: true\n", want: none, wantDisjoint: true},
		{name: "and the same the other way round", ref: "writeOnly: true\n", tgt: "readOnly: true\n", want: none, wantDisjoint: true},
		{name: "neither declares one", ref: "type: string\n", tgt: "type: string\n"},
		{name: "nothing at all", want: ir.Visibility{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ref, tgt *oas3.Schema
			if tc.ref != "" {
				ref = schemaFromYAML(t, tc.ref)
			}
			if tc.tgt != "" {
				tgt = schemaFromYAML(t, tc.tgt)
			}
			got, disjoint := EffectiveVisibility(ref, tgt)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantDisjoint, disjoint, "whether the flags left no lifecycle at all")
		})
	}
}

// TestEffectiveDeprecated_UseSiteOverridesReferent pins the same precedence for
// the deprecated flag, including the case the nil-safe read exists for: a site
// that writes no flag at all must fall through to the referent rather than
// reading its own absence as false.
func TestEffectiveDeprecated_UseSiteOverridesReferent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ref, tgt string
		want     bool
	}{
		{name: "the site says so", ref: "deprecated: true\n", want: true},
		{name: "the site says not, over a referent that does", ref: "deprecated: false\n", tgt: "deprecated: true\n"},
		{name: "an absent flag falls through", ref: "type: string\n", tgt: "deprecated: true\n", want: true},
		{name: "neither says anything", ref: "type: string\n", tgt: "type: string\n"},
		{name: "no schemas at all"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ref, tgt *oas3.Schema
			if tc.ref != "" {
				ref = schemaFromYAML(t, tc.ref)
			}
			if tc.tgt != "" {
				tgt = schemaFromYAML(t, tc.tgt)
			}
			assert.Equal(t, tc.want, EffectiveDeprecated(ref, tgt))
		})
	}
}

// TestFillTypeDocs_AssignsRatherThanAccumulates pins the "every field is
// assigned" rule. A pointer read twice — lowered in place and again through a
// $ref that hoists it — must not end up with two copies of its externalDocs.
func TestFillTypeDocs_AssignsRatherThanAccumulates(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "title: T\ndescription: D\nexternalDocs: {url: 'https://x', description: E}\n")

	var d ir.Docs
	FillTypeDocs(&d, s)
	FillTypeDocs(&d, s)

	assert.Equal(t, "T", d.Summary)
	assert.Equal(t, "D", d.Description)
	assert.Equal(t, []ir.Link{{URL: "https://x", Description: "E"}}, d.ExternalDocs)
}

// TestFillTypeDocs_AnAbsentFieldOverwritesNothing pins the other half: a schema
// that writes no title must leave one already read from a referent standing,
// which is what makes the field-by-field merge in FillCarrierDocs work.
func TestFillTypeDocs_AnAbsentFieldOverwritesNothing(t *testing.T) {
	t.Parallel()
	d := ir.Docs{Summary: "kept", Description: "kept", ExternalDocs: []ir.Link{{URL: "kept"}}}

	FillTypeDocs(&d, schemaFromYAML(t, "type: string\n"))

	assert.Equal(t, ir.Docs{Summary: "kept", Description: "kept", ExternalDocs: []ir.Link{{URL: "kept"}}}, d)
}

// TestFillCarrierDocs_MergesFieldByField pins ir-design §14 at the carrier: the
// referent's documentation arrives first and the site's overrides it one field
// at a time, so a site that writes only a description still inherits the
// referent's title rather than replacing the whole set.
func TestFillCarrierDocs_MergesFieldByField(t *testing.T) {
	t.Parallel()
	var d ir.Docs

	FillCarrierDocs(&d, schemaFromYAML(t, "description: fromSite\n"),
		schemaFromYAML(t, "title: fromTarget\ndescription: fromTarget\n"))

	assert.Equal(t, "fromTarget", d.Summary, "the title has no site-local answer")
	assert.Equal(t, "fromSite", d.Description, "the site wrote one, so it wins")
}

// TestXMLHints_ReadsEveryFieldDirectly pins the fields read off the XML object
// and the nil guard. The fields are read directly rather than through getters
// that dereference unset pointers, so a schema writing only some of them must
// not crash or invent the rest.
func TestXMLHints_ReadsEveryFieldDirectly(t *testing.T) {
	t.Parallel()
	assert.Nil(t, XMLHints(nil))

	s := schemaFromYAML(t, "type: string\nxml: {name: n, namespace: ns, prefix: p, wrapped: true, attribute: true}\n")
	assert.Equal(t, &ir.XMLHints{Name: "n", Namespace: "ns", Prefix: "p", Wrapped: true, NodeType: "attribute"},
		XMLHints(s.GetXML()))

	bare := schemaFromYAML(t, "type: string\nxml: {}\n")
	assert.Equal(t, &ir.XMLHints{}, XMLHints(bare.GetXML()), "an empty xml object invents nothing")

	notAttr := schemaFromYAML(t, "type: string\nxml: {attribute: false}\n")
	assert.Empty(t, XMLHints(notAttr.GetXML()).NodeType, "attribute: false is not the attribute node type")
}

// TestExtensionsFrom_KeepsEachAndLocatesItAtItsOwnKey pins the vendor-extension
// reader: every x-* key is kept under its own pointer with the reason that says
// the format assigns it no semantics.
func TestExtensionsFrom_KeepsEachAndLocatesItAtItsOwnKey(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-a: 1\nx-b: {k: v}\n")

	got, diags := ExtensionsFrom(s.GetExtensions(), 3, "/components/schemas/S")

	assert.Empty(t, diags)
	require.Len(t, got, 2)
	assert.Equal(t, ir.RawValue("1"), got["openapi:x-a"].Value)
	assert.Equal(t, ir.ReasonVendorExtension, got["openapi:x-a"].Reason)
	assert.Equal(t, ir.Provenance{Source: 3, Pointer: "/components/schemas/S/x-a"},
		got["openapi:x-a"].Provenance)
	assert.JSONEq(t, `{"k":"v"}`, string(got["openapi:x-b"].Value))
}

// TestExtensionsFrom_NothingWrittenIsNotAnEmptyMap pins that an absent or empty
// extension map yields no Unmodeled at all. An empty map would serialize as an
// `unmodeled: {}` the source never wrote.
func TestExtensionsFrom_NothingWrittenIsNotAnEmptyMap(t *testing.T) {
	t.Parallel()
	got, diags := ExtensionsFrom(nil, 0, "/x")
	assert.Nil(t, got)
	assert.Empty(t, diags)

	got, diags = ExtensionsFrom(schemaFromYAML(t, "type: string\n").GetExtensions(), 0, "/x")
	assert.Nil(t, got)
	assert.Empty(t, diags)
}

// TestExtensionsFrom_UnserializableIsWarnedNotKept pins the degradation path,
// including the case where it is the only extension written: the result must be
// a nil map rather than an empty one, with the warning still reported.
func TestExtensionsFrom_UnserializableIsWarnedNotKept(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-bad: .nan\n")

	got, diags := ExtensionsFrom(s.GetExtensions(), 0, "/x")

	assert.Nil(t, got, "nothing was kept, so there is no map to emit")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
	assert.Contains(t, diags[0].Message, `"x-bad"`)
}

// TestExtensionsUnder_KeysBeneathTheScope pins the scoped spelling: an object
// with no Unmodeled map of its own keys its entries under the path that says
// which object wrote them, while the entry's provenance still points at the
// extension itself.
func TestExtensionsUnder_KeysBeneathTheScope(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nx-a: 1\n")

	got, diags := ExtensionsUnder(s.GetExtensions(), 2, "/info/contact", "info/contact")

	assert.Empty(t, diags)
	require.Len(t, got, 1)
	require.Contains(t, got, "openapi:info/contact/x-a")
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/info/contact/x-a"},
		got["openapi:info/contact/x-a"].Provenance)
}

// TestExtensionsAt_FoldsEverySiteWithoutCollision is the reason the scope
// exists: two objects writing the same x-* key onto one carrier must both
// survive, which one unscoped key cannot do.
func TestExtensionsAt_FoldsEverySiteWithoutCollision(t *testing.T) {
	t.Parallel()
	info := schemaFromYAML(t, "type: string\nx-a: 1\n")
	license := schemaFromYAML(t, "type: string\nx-a: 2\n")

	got, diags := ExtensionsAt(0,
		ExtensionSite{Scope: "info", Owner: "/info", Ext: info.GetExtensions()},
		ExtensionSite{Scope: "info/license", Owner: "/info/license", Ext: license.GetExtensions()},
		ExtensionSite{Scope: "components", Owner: "/components", Ext: nil},
	)

	assert.Empty(t, diags)
	require.Len(t, got, 2, "one key spelling, two objects, two entries")
	assert.Equal(t, ir.RawValue("1"), got["openapi:info/x-a"].Value)
	assert.Equal(t, ir.RawValue("2"), got["openapi:info/license/x-a"].Value)
}

// TestExtensionsAt_ReportsEverySiteThatFailed holds the diagnostics to the same
// completeness as the entries: a site whose extension cannot be serialized keeps
// nothing, and the fold must still carry its warning out (GitHub #144's rule,
// applied to the multi-site form).
func TestExtensionsAt_ReportsEverySiteThatFailed(t *testing.T) {
	t.Parallel()
	bad := schemaFromYAML(t, "type: string\nx-bad: .nan\n")

	got, diags := ExtensionsAt(0, ExtensionSite{Scope: "info", Owner: "/info", Ext: bad.GetExtensions()})

	assert.Nil(t, got, "nothing was kept, so there is no map to emit")
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityWarning, diags[0].Severity)
}

// TestMergeUnmodeled_AllocatesOnlyOnFirstWrite pins the overlay: merging nothing
// leaves the destination exactly as it was — including nil, which must not
// become an empty map.
func TestMergeUnmodeled_AllocatesOnlyOnFirstWrite(t *testing.T) {
	t.Parallel()
	assert.Nil(t, MergeUnmodeled(nil, nil))
	assert.Nil(t, MergeUnmodeled(nil, ir.Unmodeled{}))

	src := ir.Unmodeled{"a": {Reason: ir.ReasonOutOfScope}}
	assert.Equal(t, src, MergeUnmodeled(nil, src), "a nil destination is allocated to hold the first entry")

	dst := ir.Unmodeled{"b": {Reason: ir.ReasonNoIRHome}}
	assert.Equal(t, ir.Unmodeled{
		"a": {Reason: ir.ReasonOutOfScope}, "b": {Reason: ir.ReasonNoIRHome},
	}, MergeUnmodeled(dst, src))
}

// TestIsFalseSchema_OnlyTheFalseBoolean pins the predicate that distinguishes a
// structural mode from a schema. Reading `true` or an object schema as the false
// one would turn a permissive position into a forbidding one.
func TestIsFalseSchema_OnlyTheFalseBoolean(t *testing.T) {
	t.Parallel()
	assert.True(t, IsFalseSchema(jsonSchemaFromYAML(t, "false\n")))
	assert.False(t, IsFalseSchema(jsonSchemaFromYAML(t, "true\n")))
	assert.False(t, IsFalseSchema(jsonSchemaFromYAML(t, "type: string\n")))
	assert.False(t, IsFalseSchema(nil))
}

// TestCombinedRaw_JoinsOnlyWhatIsWritten pins the three combining readers. Each
// keyword present goes into one object in the requested order, and a
// combination with nothing written yields nothing rather than an empty object.
func TestCombinedRaw_JoinsOnlyWhatIsWritten(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		read func(*oas3.Schema) (ir.RawValue, error)
		want string
	}{
		{
			name: "every if/then/else arm", read: IfThenElseRaw,
			body: "if: {type: string}\nthen: {maxLength: 2}\nelse: {type: integer}\n",
			want: `{"if":{"type":"string"},"then":{"maxLength":2},"else":{"type":"integer"}}`,
		},
		{name: "no arms at all", read: IfThenElseRaw, body: "type: string\n"},
		{
			name: "contains with both bounds", read: ContainsRaw,
			body: "contains: {type: string}\nminContains: 1\nmaxContains: 3\n",
			want: `{"contains":{"type":"string"},"minContains":1,"maxContains":3}`,
		},
		{name: "no contains family", read: ContainsRaw, body: "type: array\n"},
		{
			name: "both unevaluated keywords", read: UnevaluatedRaw,
			body: "unevaluatedProperties: {type: string}\nunevaluatedItems: {type: integer}\n",
			want: `{"unevaluatedProperties":{"type":"string"},"unevaluatedItems":{"type":"integer"}}`,
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
			got, err := tc.read(schemaFromYAML(t, tc.body))
			require.NoError(t, err)
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
// instead.
func TestCombinedRaw_AnUnconvertibleMemberFailsTheWhole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		read func(*oas3.Schema) (ir.RawValue, error)
	}{
		{name: "if/then/else", body: "if: {type: string}\nthen: {const: .nan}\n", read: IfThenElseRaw},
		{name: "contains", body: "contains: {const: .nan}\nminContains: 1\n", read: ContainsRaw},
		{name: "unevaluated", body: "unevaluatedProperties: {type: string}\nunevaluatedItems: {const: .nan}\n", read: UnevaluatedRaw},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.read(schemaFromYAML(t, tc.body))
			require.Error(t, err, "the whole combination fails rather than dropping a member")
			assert.Nil(t, got)
		})
	}
}

// TestRawFromNode_DistinguishesAbsentFromUnconvertible pins the three outcomes
// the reader exists to keep apart. Collapsing the first two into one nil return
// is what let a diagnostic announce a preservation that never happened.
func TestRawFromNode_DistinguishesAbsentFromUnconvertible(t *testing.T) {
	t.Parallel()
	got, err := RawFromNode(nil)
	require.NoError(t, err)
	assert.Nil(t, got, "an absent node is not a failure")

	got, err = RawFromNode(openapitest.YAMLNode(t, "{a: 1}"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1}`, string(got))

	got, err = RawFromNode(openapitest.YAMLNode(t, ".nan"))
	require.Error(t, err, "a value that decodes but does not marshal is a failure")
	assert.Nil(t, got)

	got, err = RawFromNode(openapitest.YAMLNode(t, "{? [1, 2]\n: v}"))
	require.Error(t, err, "a mapping with a non-string key does not decode into the JSON model")
	assert.Nil(t, got)
}

// TestRawChildNode_ReadsOnlyAMappingChild pins the raw reader every verbatim
// keyword goes through. Reading a key off something that is not a mapping, or
// failing to step through a document node, would report a keyword as absent that
// the source did write.
func TestRawChildNode_ReadsOnlyAMappingChild(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a: 1\n"), &doc))

	require.Equal(t, yaml.DocumentNode, doc.Kind)
	found := RawChildNode(&doc, "a")
	require.NotNil(t, found, "a document node is stepped through to its mapping")
	assert.Equal(t, "1", found.Value)

	assert.Nil(t, RawChildNode(nil, "a"))
	assert.Nil(t, RawChildNode(&doc, "absent"))
	assert.Nil(t, RawChildNode(openapitest.YAMLNode(t, "[1, 2]"), "a"), "a sequence has no keyed children")
	assert.Nil(t, RawChildNode(openapitest.YAMLNode(t, "plain"), "a"), "nor does a scalar")
	assert.Nil(t, RawChildNode(&yaml.Node{Kind: yaml.DocumentNode}, "a"), "nor an empty document")
}

// TestRawPropertyNode_NilSchemaReadsNothing pins the nil guard on the schema
// side of the same reader, which every caller relies on to ask about a position
// that may have no body written at it.
func TestRawPropertyNode_NilSchemaReadsNothing(t *testing.T) {
	t.Parallel()
	assert.Nil(t, RawPropertyNode(nil, "type"))

	s := schemaFromYAML(t, "type: string\n")
	require.NotNil(t, RawPropertyNode(s, "type"))
	assert.Nil(t, RawPropertyNode(s, "title"), "a keyword the schema did not write")
}

// TestDeclaresAny_AsksTheRawNodes pins the gate's agreement with the recorders it
// gates: it must answer from the same raw nodes they read, so a keyword one sees
// and the other does not cannot arise.
func TestDeclaresAny_AsksTheRawNodes(t *testing.T) {
	t.Parallel()
	assert.True(t, DeclaresAny(schemaFromYAML(t, "$id: 'urn:a'\ntype: string\n"), DialectKeywords))
	assert.False(t, DeclaresAny(schemaFromYAML(t, "type: string\n"), DialectKeywords))
	assert.False(t, DeclaresAny(schemaFromYAML(t, "type: string\n"), nil))
}

// TestPreserveInto_RecordsOnlyRealBytes pins the len check rather than a nil
// comparison: an empty payload preserves no construct, and json.Marshal rejects
// it for the whole document while naming json.RawMessage rather than the entry
// that carried it.
func TestPreserveInto_RecordsOnlyRealBytes(t *testing.T) {
	t.Parallel()
	var p ir.Unmodeled
	PreserveInto(&p, "openapi:x", nil, ir.ReasonOutOfScope, "/x", 0)
	assert.Nil(t, p, "an absent payload allocates nothing")

	PreserveInto(&p, "openapi:x", ir.RawValue(""), ir.ReasonOutOfScope, "/x", 0)
	assert.Nil(t, p, "nor does an empty one")

	PreserveInto(&p, "openapi:x", ir.RawValue("1"), ir.ReasonOutOfScope, "/x", 7)
	assert.Equal(t, ir.Unmodeled{"openapi:x": {
		Reason: ir.ReasonOutOfScope, Value: ir.RawValue("1"),
		Provenance: ir.Provenance{Source: 7, Pointer: "/x"},
	}}, p)
}

// TestPreserveNodeInto_ReportsWhichOfThreeOutcomesHappened pins the bool its
// callers gate their announcement on. An absent node must report false with no
// diagnostic, and an unconvertible one false with the diagnostic that says
// nothing reached the IR.
func TestPreserveNodeInto_ReportsWhichOfThreeOutcomesHappened(t *testing.T) {
	t.Parallel()
	var p ir.Unmodeled

	kept, diags := PreserveNodeInto(&p, "openapi:x", nil, ir.ReasonNoIRHome, "/x", 0)
	assert.False(t, kept)
	assert.Empty(t, diags)
	assert.Nil(t, p)

	kept, diags = PreserveNodeInto(&p, "openapi:x", openapitest.YAMLNode(t, ".nan"), ir.ReasonNoIRHome, "/x", 0)
	assert.False(t, kept)
	require.Len(t, diags, 1)
	assert.Equal(t, ir.SeverityError, diags[0].Severity)
	assert.Nil(t, p, "an unconvertible node writes no entry")

	kept, diags = PreserveNodeInto(&p, "openapi:x", openapitest.YAMLNode(t, "{a: 1}"), ir.ReasonNoIRHome, "/x", 0)
	assert.True(t, kept)
	assert.Empty(t, diags)
	assert.JSONEq(t, `{"a":1}`, string(p["openapi:x"].Value))
}

// TestUnpreservableDiag_IsAnErrorNotADegradation pins the severity split: a
// degraded lowering still describes the source in a weaker shape, while this one
// leaves no trace at all, which is a losslessness failure.
func TestUnpreservableDiag_IsAnErrorNotADegradation(t *testing.T) {
	t.Parallel()
	_, err := RawFromNode(openapitest.YAMLNode(t, ".nan"))
	require.Error(t, err)

	got := UnpreservableDiag("openapi:not", "/components/schemas/S/not", 2, err)

	assert.Equal(t, ir.SeverityError, got.Severity)
	assert.Equal(t, ir.Provenance{Source: 2, Pointer: "/components/schemas/S/not"}, got.Provenance)
	assert.Contains(t, got.Message, "in no form at all")
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

	got, diags := validationOnlyAt(s, "/components/schemas/S", 0)

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

	got, diags := validationOnlyAt(s, "/components/schemas/S", 0)

	assert.NotContains(t, got, "openapi:not")
	assert.NotContains(t, got, "openapi:contains")
	require.GreaterOrEqual(t, len(diags), 2)
	for _, d := range diags {
		assert.Equal(t, ir.SeverityError, d.Severity, "an unconvertible keyword is an error")
	}
}

// TestSchemaExamplesAt_ReadsBothKeywordsInOrder pins that `example` and each
// entry of `examples` are read, since a position may write either and the two
// are separate keywords rather than aliases.
func TestSchemaExamplesAt_ReadsBothKeywordsInOrder(t *testing.T) {
	t.Parallel()
	s := schemaFromYAML(t, "type: string\nexample: one\nexamples: [two, three]\n")

	got, diags := schemaExamplesAt(s, "/components/schemas/S", 0)

	assert.Empty(t, diags)
	require.Len(t, got, 3)
	for i, want := range []string{"one", "two", "three"} {
		require.NotNil(t, got[i].Value)
		assert.Equal(t, want, got[i].Value.Str, "example %d", i)
	}
}
