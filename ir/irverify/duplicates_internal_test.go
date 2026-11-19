// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
)

// duplicateViolations runs the duplicate check and drops the truncation flag,
// which the cases below assert nothing about.
func duplicateViolations(doc *ir.Document) []Violation {
	vs, _ := runCheck(checkDuplicateIDs, doc)
	return vs
}

// docWithOperations wraps the given operations in one service and group.
func docWithOperations(ops ...ir.Operation) *ir.Document {
	return &ir.Document{Services: []ir.Service{{
		ID:     "service/x",
		Groups: []ir.OperationGroup{{ID: "operation-group/x", Operations: ops}},
	}}}
}

func TestCheckDuplicateIDs_TwoOperationsOnOneID(t *testing.T) {
	doc := docWithOperations(ir.Operation{ID: "operation/x"}, ir.Operation{ID: "operation/x"})

	got := duplicateViolations(doc)
	require.Len(t, got, 1, "the first declaration stands and only the second is reported")
	assert.Equal(t, "ir/duplicate-operation-id", got[0].Code)
	assert.Equal(t, "document.Services[0].Groups[0].Operations[1]", got[0].Path)
	assert.Contains(t, got[0].Message, "document.Services[0].Groups[0].Operations[0]",
		"the message names the declaration this one collides with")
}

func TestCheckDuplicateIDs_ThreeOperationsOnOneIDReportTwo(t *testing.T) {
	doc := docWithOperations(
		ir.Operation{ID: "operation/x"}, ir.Operation{ID: "operation/x"}, ir.Operation{ID: "operation/x"})

	got := duplicateViolations(doc)
	require.Len(t, got, 2, "n nodes on one ID are n-1 violations, not n")
	assert.Equal(t, "document.Services[0].Groups[0].Operations[1]", got[0].Path)
	assert.Equal(t, "document.Services[0].Groups[0].Operations[2]", got[1].Path)
}

func TestCheckDuplicateIDs_TwoServicesOnOneID(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "service/x"}, {ID: "service/x"}}}

	got := duplicateViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/duplicate-service-id", got[0].Code)
	assert.Equal(t, "document.Services[1]", got[0].Path)
}

// TestCheckDuplicateIDs_GroupIDsAreUniqueAcrossTheWholeDocument pins how far a
// group ID's uniqueness reaches, which is as far as a consumer keys by it: the
// second of two siblings, a group nested under another, and a group in another
// service each collide with the first. A sibling-only check would report one.
func TestCheckDuplicateIDs_GroupIDsAreUniqueAcrossTheWholeDocument(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{
		{ID: "service/a", Groups: []ir.OperationGroup{
			{ID: "operation-group/x", Groups: []ir.OperationGroup{{ID: "operation-group/x"}}},
			{ID: "operation-group/x"},
		}},
		{ID: "service/b", Groups: []ir.OperationGroup{{ID: "operation-group/x"}}},
	}}

	got := duplicateViolations(doc)
	paths := make([]string, 0, len(got))
	for _, violation := range got {
		assert.Equal(t, "ir/duplicate-group-id", violation.Code)
		paths = append(paths, violation.Path)
	}
	assert.Equal(t, []string{
		"document.Services[0].Groups[0].Groups[0]",
		"document.Services[0].Groups[1]",
		"document.Services[1].Groups[0]",
	}, paths, "each later declaration is reported against the first")
}

// TestCheckDuplicateIDs_TwoRegistryEntriesOnOneNodeID drives a map-keyed class.
// Two entries cannot share a key, but they can hold nodes claiming one ID, and
// then a reference to it resolves to whichever the reader reaches first.
func TestCheckDuplicateIDs_TwoRegistryEntriesOnOneNodeID(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"type/x/A": &ir.Any{ID: "type/x/Clash"},
		"type/x/B": &ir.Any{ID: "type/x/Clash"},
	}}

	got := duplicateViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/duplicate-type-id", got[0].Code)
	assert.Contains(t, got[0].Message, "type/x/Clash")
}

// TestCheckDuplicateIDs_DistinctIDsAreClean is the other half of the proof: a
// check that cannot stay silent is no better than one that cannot fire. The
// operations differ only in their IDs, so nothing but the ID can be what the
// check reads.
func TestCheckDuplicateIDs_DistinctIDsAreClean(t *testing.T) {
	assert.Empty(t, duplicateViolations(
		docWithOperations(ir.Operation{ID: "operation/x"}, ir.Operation{ID: "operation/y"})))
}

// TestCheckDuplicateIDs_RepeatedPropIDIsClean pins the carve-out. A response
// declared once in components and referenced by two operations materializes into
// both — responses are embedded by value, not interned — so the header property
// it declares appears at two paths under the one ID its defining occurrence
// derives. The copies are the same property, and reporting them would fail every
// document that reuses a component (see checkDuplicateIDs).
func TestCheckDuplicateIDs_RepeatedPropIDIsClean(t *testing.T) {
	header := ir.Header{ID: "property/x/Listed/headers/X-Rate", Name: ir.Naming{Source: "X-Rate"}}
	shared := ir.Response{Name: ir.Naming{Source: "Listed"}, Headers: []ir.Header{header}}
	doc := docWithOperations(
		ir.Operation{ID: "operation/a", Responses: []ir.Response{shared}},
		ir.Operation{ID: "operation/b", Responses: []ir.Response{shared}},
	)

	assert.Empty(t, duplicateViolations(doc))
}

// modelWithProp wraps one property in a model of its own, so two of them collide
// on the PropertyID and on nothing else.
func modelWithProp(id ir.TypeID, prop ir.Property) *ir.Model {
	return &ir.Model{ID: id, Properties: []ir.Property{prop}}
}

// TestCheckDuplicateIDs_TwoDifferentPropertiesOnOnePropID is what the class-wide
// skip hid alongside the copies above: two properties that are not copies of one
// declaration, minted at one ID. Every PropertyID lookup downstream then resolves to
// whichever the reader reaches first, with nothing in the document saying which
// that is.
//
// One subtest per fingerprint component, because a component the fingerprint
// stopped reading would leave that pair silently indistinguishable — and this
// check would go on passing its other cases.
func TestCheckDuplicateIDs_TwoDifferentPropertiesOnOnePropID(t *testing.T) {
	const dup ir.PropertyID = "property/x/dup"
	base := ir.Property{ID: dup, Name: ir.Naming{Source: "alpha"}, WireName: "alpha",
		Type: ir.TypeRef{Target: "type/x/A"}}

	differs := map[string]func(property ir.Property) ir.Property{
		"source name": func(property ir.Property) ir.Property { property.Name = ir.Naming{Source: "beta"}; return property },
		"wire name":   func(property ir.Property) ir.Property { property.WireName = "beta"; return property },
		"type": func(property ir.Property) ir.Property {
			property.Type = ir.TypeRef{Target: "type/x/B"}
			return property
		},
	}
	for field, differ := range differs {
		t.Run(field, func(t *testing.T) {
			doc := &ir.Document{Types: ir.TypeRegistry{
				"type/x/A": modelWithProp("type/x/A", base),
				"type/x/B": modelWithProp("type/x/B", differ(base)),
			}}

			got := duplicateViolations(doc)
			require.Len(t, got, 1, "the first declaration stands and only the second is reported")
			assert.Equal(t, "ir/duplicate-property-id", got[0].Code)
			assert.Equal(t, "document.Types[type/x/B].Properties[0]", got[0].Path)
			assert.Contains(t, got[0].Message, string(dup))
			assert.Contains(t, got[0].Message, "document.Types[type/x/A].Properties[0]")
		})
	}
}

// TestCheckDuplicateIDs_CopiesDifferingOutsideTheFingerprintAreClean holds the
// fingerprint to being no wider than it has to be, which the corpus cannot: the
// three copies in component-reuse.yaml are equal in every field today, so a
// fingerprint over the whole property would pass. A later lowering may differ
// in a position-carried field such as provenance or a required flag, and a
// wider fingerprint would then report every document that reuses a component.
// Only what a duplicate ID costs is read: which property a lookup for it
// reaches.
func TestCheckDuplicateIDs_CopiesDifferingOutsideTheFingerprintAreClean(t *testing.T) {
	base := ir.Property{ID: "property/x/dup", Name: ir.Naming{Source: "alpha"}, WireName: "alpha",
		Type: ir.TypeRef{Target: "type/x/A"}}
	elsewhere := base
	elsewhere.Provenance = ir.Provenance{Source: ir.NoSource, Pointer: "/paths/~1b/get"}
	elsewhere.Required = true
	elsewhere.Docs = ir.Docs{Summary: "copied into a second position"}

	doc := &ir.Document{Types: ir.TypeRegistry{
		"type/x/A": modelWithProp("type/x/A", base),
		"type/x/B": modelWithProp("type/x/B", elsewhere),
	}}

	assert.Empty(t, duplicateViolations(doc))
}

// TestCheckDuplicateIDs_EachRepeatIsHeldToTheFirstProperty pins what a later
// property on a PropertyID is compared with: the first one the walk meets, and not
// the one met just before it, nor the PropertyID as a whole. Each copy and each
// difference is placed by its own path, in the walk's order of the models.
func TestCheckDuplicateIDs_EachRepeatIsHeldToTheFirstProperty(t *testing.T) {
	t.Parallel()
	const dup ir.PropertyID = "property/x/dup"
	alpha := ir.Property{ID: dup, Name: ir.Naming{Source: "alpha"}, WireName: "alpha", Type: ir.TypeRef{Target: "type/x/A"}}
	beta := alpha
	beta.WireName = "beta"
	for _, tc := range []struct {
		name  string
		props []ir.Property // one per model, met in this order
		want  []string
	}{
		{"a copy, then a difference", []ir.Property{alpha, alpha, beta}, []string{"document.Types[type/x/C].Properties[0]"}},
		{"a difference, then its copy", []ir.Property{alpha, beta, beta},
			[]string{"document.Types[type/x/B].Properties[0]", "document.Types[type/x/C].Properties[0]"}},
		{"copies only", []ir.Property{alpha, alpha, alpha}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{Types: ir.TypeRegistry{}}
			for i, prop := range tc.props {
				id := ir.TypeID("type/x/" + string(rune('A'+i)))
				doc.Types[id] = modelWithProp(id, prop)
			}
			got := duplicateViolations(doc)
			paths := make([]string, 0, len(got))
			for _, violation := range got {
				assert.Equal(t, "ir/duplicate-property-id", violation.Code)
				assert.Contains(t, violation.Message, "document.Types[type/x/A].Properties[0]", "held to the first property")
				paths = append(paths, violation.Path)
			}
			assert.Equal(t, tc.want, paths)
		})
	}
}

// TestCheckDuplicateIDs_VisitingCostsNoAllocation holds the check's cost for a
// property at nothing however often its PropertyID repeats, as every copy of a
// reused component does: a fingerprint holds the property's own strings, and a
// path is rendered only for a property whose fingerprint is not the first's.
// It is not run in parallel: AllocsPerRun measures the process.
func TestCheckDuplicateIDs_VisitingCostsNoAllocation(t *testing.T) {
	header := ir.Header{ID: "property/x/Listed/headers/X-Rate", Name: ir.Naming{Source: "X-Rate"}, WireName: "X-Rate",
		Type: ir.TypeRef{Target: "type/x/Rate"}}
	shared := ir.Response{Name: ir.Naming{Source: "Listed"}, Headers: []ir.Header{header}}
	doc := docWithOperations(
		ir.Operation{ID: "operation/a", Responses: []ir.Response{shared}},
		ir.Operation{ID: "operation/b", Responses: []ir.Response{shared}},
		ir.Operation{ID: "operation/c", Responses: []ir.Response{shared}},
	)
	doc.Types = ir.TypeRegistry{"type/x/M": &ir.Model{ID: "type/x/M", Properties: []ir.Property{
		{ID: "property/x/M/a", Name: ir.Naming{Source: "a"}, WireName: "a", Type: ir.TypeRef{Target: "type/x/A"}},
		{ID: "property/x/M/b", Name: ir.Naming{Source: "b"}, WireName: "b", Type: ir.TypeRef{Target: "type/x/B"}},
	}}}
	check := checkDuplicateIDs(doc)
	assert.Empty(t, irtest.AllocatingVisits(doc, check))
	assert.Empty(t, duplicateViolations(doc), "the header's copies are one property")
}

// TestPropIDs_AreDeclaredByPropertiesAlone pins what checkDuplicateIDs leans on
// when it lets a repeated PropertyID through: every node declaring one is an
// ir.Property, the one node it fingerprints. A second node declaring a PropertyID
// would repeat it unfingerprinted and pass for a copy, so adding one reddens
// this, for the check to decide what tells two of them apart.
func TestPropIDs_AreDeclaredByPropertiesAlone(t *testing.T) {
	t.Parallel()
	var declaring []string
	for _, declaredField := range declaredFields(t) {
		named, isNamed := types.Unalias(declaredField.field.Type()).(*types.Named)
		if declaredField.field.Name() == "ID" && isNamed && named.Obj().Name() == propIDType.Name() {
			declaring = append(declaring, declaredField.owner)
		}
	}
	assert.Equal(t, []string{propertyType.Name()}, declaring)
}

// TestVerify_ReportsDuplicateIDs pins that Verify runs the check, not just the
// test: an ambiguous identity has to reach a caller that only ever calls Verify.
func TestVerify_ReportsDuplicateIDs(t *testing.T) {
	doc := docWithOperations(ir.Operation{ID: "operation/x"}, ir.Operation{ID: "operation/x"})

	codes := make([]string, 0, 4)
	for _, violation := range Verify(doc) {
		codes = append(codes, violation.Code)
	}
	assert.Contains(t, codes, "ir/duplicate-operation-id")
}

// identityClasses classifies every named string type the ir package declares as
// an identity or not, and for an identity, what resolves its references and
// holds it to being declared once.
//
// Both checkers reach a reference by its Go type, so a class nothing resolves
// against goes unchecked in silence, as every OperationID and ServiceID reference once
// did (GitHub #50). Nothing derives the answer: a class with no map looks like
// one nobody has reached, and an enum looks like an identity. The test below
// fails when ir grows a type this does not account for.
var identityClasses = map[string]string{
	"TypeID":       "identity: Document.Types keys it; checkReferentialIntegrity resolves references, checkRegistryKeys holds each key to its node's own ID and checkDuplicateIDs holds no two nodes to one ID",
	"ChannelID":    "identity: Document.Channels keys it; resolved and held as TypeID is",
	"MessageID":    "identity: Document.Messages keys it; resolved and held as TypeID is",
	"ServerID":     "identity: Document.Servers keys it; resolved and held as TypeID is",
	"AuthID":       "identity: Document.Auth keys it; resolved and held as TypeID is",
	"OperationID":  "identity, no map: ir.IDReferences resolves references against the operations the document declares, checkDuplicateIDs holds them unique",
	"ServiceID":    "identity, no map: resolved and held as OpID is, against the services the document declares",
	"GroupID":      "identity, no map: resolved and held as OpID is, against the groups the document declares, nested ones included",
	"ParameterID":  "identity, operation-scoped: ir.IDReferences resolves references against the parameters the document declares, and checkDuplicateIDs holds every parameter to one ID with no fingerprint exemption, since a path-item parameter is one parameter per operation it is copied into",
	"EnumMemberID": "identity, enum-scoped: checkDuplicateIDs holds every member to one ID with no fingerprint exemption, checkMemberIDs holds it under its enum's path and pass.Validate rejects a repeat within one enum (checkDuplicateMemberIDs)",
	"PropertyID":   "identity, model-scoped: pass.Validate resolves references (checkPropIDRefs, checkEncodingKeys); checkDuplicateIDs holds no two *different* properties to one ID, the copies a component makes of one property being exempt by fingerprint",

	"BigVal":           "arbitrary-precision decimal, not an identity",
	"Inferred":         "the heuristics that produced a fact, not an identity",
	"PrimitiveKind":    "primitive leaf kind; ir.PrimTypeID derives an ID from it, but the kind is not one",
	"TypeKind":         "sum-type tag on TypeDef, not an identity",
	"ValueKind":        "sum-type tag on Value, not an identity",
	"AuthKind":         "auth scheme class, not an identity",
	"AdditionalMode":   "open/closed/constrained additional-property mode, not an identity",
	"PresenceKind":     "wire-presence discipline, not an identity",
	"Lifecycle":        "visibility lifecycle class; an alias of string, so no reflect.Type of its own",
	"Stability":        "how far a source commits to an operation's contract, not an identity",
	"StreamingMode":    "streaming direction, not an identity",
	"IdempotencyKind":  "idempotency class, not an identity",
	"PageStop":         "a condition that ends pagination, not an identity",
	"LinkValueKind":    "where a link reads a value from, not an identity",
	"PageStrategy":     "pagination mechanism, not an identity",
	"HTTPInbound":      "who sends an HTTP operation, the client or the API, not an identity",
	"HTTPLocation":     "HTTP wire location of a parameter, not an identity",
	"MessageDirection": "send/receive perspective, not an identity",
	"UnmodeledReason":  "why an Unmodeled entry was kept, not an identity",
	"Severity":         "diagnostic severity, not an identity",
	"RelatedRole":      "what a diagnostic's related entry is to the finding, not an identity",
	"Undistributed":    "why ir.NarrowedShapes lists no shape of a model narrowing a union, not an identity",
	"treeClass":        "which kind of node of the service tree an ir.Editor holds, its own state and not an identity",
}

// TestIdentityClasses_AreAllClassified fails when ir declares a named string type
// identityClasses does not account for. A new class of ID is invisible to both
// checkers' reflection walks until something resolves it, and nothing else would
// notice one going unresolved.
func TestIdentityClasses_AreAllClassified(t *testing.T) {
	t.Parallel()
	found := declaredStringTypes(t)
	require.NotEmpty(t, found, "the ir package must declare named string types")
	for _, name := range found {
		assert.Contains(t, identityClasses, name,
			"ir.%s is a named string type identityClasses does not classify: "+
				"say whether it is a class of ID (and resolve references to it) or not", name)
	}
	assert.Len(t, identityClasses, len(found),
		"identityClasses classifies a type the ir package no longer declares")
}

// declaredStringTypes returns the name of every type the ir package declares
// over a string, defined or aliased, including one defined over another string
// type. An alias of a type declared elsewhere adds no type of its own and is not
// listed.
func declaredStringTypes(t *testing.T) []string {
	t.Helper()
	var names []string
	for decl := range irDecls(t).Objects[*types.TypeName]() {
		if declaresStringType(decl) {
			names = append(names, decl.Name())
		}
	}
	return names
}

// declaresStringType reports whether decl declares a type whose underlying type
// is a string.
func declaresStringType(decl *types.TypeName) bool {
	if aliasesAnother(decl) {
		return false
	}
	basic, isBasic := decl.Type().Underlying().(*types.Basic)
	return isBasic && basic.Info()&types.IsString != 0
}
