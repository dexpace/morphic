package irverify

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

// duplicateViolations runs the duplicate check and drops the truncation flag,
// which the cases below assert nothing about; TestWalkChecks_EachReportsTruncation
// holds that half.
func duplicateViolations(doc *ir.Document) []Violation {
	vs, _ := checkDuplicateIDs(doc, readDeclarations(doc))
	return vs
}

// docWithOperations wraps the given operations in one service and group.
func docWithOperations(ops ...ir.Operation) *ir.Document {
	return &ir.Document{Services: []ir.Service{{
		ID:     "s/x",
		Groups: []ir.OperationGroup{{Operations: ops}},
	}}}
}

func TestCheckDuplicateIDs_TwoOperationsOnOneID(t *testing.T) {
	doc := docWithOperations(ir.Operation{ID: "op/x"}, ir.Operation{ID: "op/x"})

	got := duplicateViolations(doc)
	require.Len(t, got, 1, "the first declaration stands and only the second is reported")
	assert.Equal(t, "ir/duplicate-op-id", got[0].Code)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[1]", got[0].Path)
	assert.Contains(t, got[0].Message, "doc.Services[0].Groups[0].Operations[0]",
		"the message names the declaration this one collides with")
}

func TestCheckDuplicateIDs_ThreeOperationsOnOneIDReportTwo(t *testing.T) {
	doc := docWithOperations(
		ir.Operation{ID: "op/x"}, ir.Operation{ID: "op/x"}, ir.Operation{ID: "op/x"})

	got := duplicateViolations(doc)
	require.Len(t, got, 2, "n nodes on one ID are n-1 violations, not n")
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[1]", got[0].Path)
	assert.Equal(t, "doc.Services[0].Groups[0].Operations[2]", got[1].Path)
}

func TestCheckDuplicateIDs_TwoServicesOnOneID(t *testing.T) {
	doc := &ir.Document{Services: []ir.Service{{ID: "s/x"}, {ID: "s/x"}}}

	got := duplicateViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/duplicate-service-id", got[0].Code)
	assert.Equal(t, "doc.Services[1]", got[0].Path)
}

// TestCheckDuplicateIDs_TwoRegistryEntriesOnOneNodeID drives a map-keyed class.
// Two entries cannot share a key, but they can hold nodes claiming one ID, and
// then a reference to it resolves to whichever the reader reaches first.
func TestCheckDuplicateIDs_TwoRegistryEntriesOnOneNodeID(t *testing.T) {
	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/x/A": &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x/Clash"}},
		"t/x/B": &ir.Any{TypeCommon: ir.TypeCommon{ID: "t/x/Clash"}},
	}}

	got := duplicateViolations(doc)
	require.Len(t, got, 1)
	assert.Equal(t, "ir/duplicate-type-id", got[0].Code)
	assert.Contains(t, got[0].Message, "t/x/Clash")
}

// TestCheckDuplicateIDs_DistinctIDsAreClean is the other half of the proof: a
// check that cannot stay silent is no better than one that cannot fire. The
// operations differ only in their IDs, so nothing but the ID can be what the
// check reads.
func TestCheckDuplicateIDs_DistinctIDsAreClean(t *testing.T) {
	assert.Empty(t, duplicateViolations(
		docWithOperations(ir.Operation{ID: "op/x"}, ir.Operation{ID: "op/y"})))
}

// TestCheckDuplicateIDs_RepeatedPropIDIsClean pins the carve-out. A response
// declared once in components and referenced by two operations materializes into
// both — responses are embedded by value, not interned — so the header property
// it declares appears at two paths under the one ID its defining occurrence
// derives. The copies are the same property, and reporting them would fail every
// document that reuses a component (see checkDuplicateIDs).
func TestCheckDuplicateIDs_RepeatedPropIDIsClean(t *testing.T) {
	header := ir.Property{ID: "p/x/Listed/headers/X-Rate", Name: ir.Naming{Source: "X-Rate"}}
	shared := ir.Response{Name: ir.Naming{Source: "Listed"}, Headers: []ir.Property{header}}
	doc := docWithOperations(
		ir.Operation{ID: "op/a", Responses: []ir.Response{shared}},
		ir.Operation{ID: "op/b", Responses: []ir.Response{shared}},
	)

	assert.Empty(t, duplicateViolations(doc))
}

// modelWithProp wraps one property in a model of its own, so two of them collide
// on the PropID and on nothing else.
func modelWithProp(id ir.TypeID, prop ir.Property) *ir.Model {
	return &ir.Model{TypeCommon: ir.TypeCommon{ID: id}, Properties: []ir.Property{prop}}
}

// TestCheckDuplicateIDs_TwoDifferentPropertiesOnOnePropID is what the class-wide
// skip hid alongside the copies above: two properties that are not copies of one
// declaration, minted at one ID. Every PropID lookup downstream then resolves to
// whichever the reader reaches first, with nothing in the document saying which
// that is.
//
// One subtest per fingerprint component, because a component the fingerprint
// stopped reading would leave that pair silently indistinguishable — and this
// check would go on passing its other cases.
func TestCheckDuplicateIDs_TwoDifferentPropertiesOnOnePropID(t *testing.T) {
	const dup ir.PropID = "p/x/dup"
	base := ir.Property{ID: dup, Name: ir.Naming{Source: "alpha"}, WireName: "alpha",
		Type: ir.TypeRef{Target: "t/x/A"}}

	differs := map[string]func(p ir.Property) ir.Property{
		"source name": func(p ir.Property) ir.Property { p.Name = ir.Naming{Source: "beta"}; return p },
		"wire name":   func(p ir.Property) ir.Property { p.WireName = "beta"; return p },
		"type":        func(p ir.Property) ir.Property { p.Type = ir.TypeRef{Target: "t/x/B"}; return p },
	}
	for field, differ := range differs {
		t.Run(field, func(t *testing.T) {
			doc := &ir.Document{Types: ir.TypeRegistry{
				"t/x/A": modelWithProp("t/x/A", base),
				"t/x/B": modelWithProp("t/x/B", differ(base)),
			}}

			got := duplicateViolations(doc)
			require.Len(t, got, 1, "the first declaration stands and only the second is reported")
			assert.Equal(t, "ir/duplicate-prop-id", got[0].Code)
			assert.Equal(t, "doc.Types[t/x/B].Properties[0]", got[0].Path)
			assert.Contains(t, got[0].Message, string(dup))
			assert.Contains(t, got[0].Message, "doc.Types[t/x/A].Properties[0]")
		})
	}
}

// TestCheckDuplicateIDs_CopiesDifferingOutsideTheFingerprintAreClean holds the
// fingerprint to being no wider than it has to be, which is a claim the corpus
// cannot make: today the three copies in component-reuse.yaml are equal in every
// field, so a fingerprint over the whole property would pass it. Nothing
// guarantees they stay that way — a position-carried field such as provenance or
// a required flag is exactly what a later lowering would differ on — and the
// day one does, a wider fingerprint reports every document that reuses a
// component, which is the failure the class-wide skip was avoiding. Only what a
// duplicate ID actually costs is read: which property a lookup for that ID
// reaches.
func TestCheckDuplicateIDs_CopiesDifferingOutsideTheFingerprintAreClean(t *testing.T) {
	base := ir.Property{ID: "p/x/dup", Name: ir.Naming{Source: "alpha"}, WireName: "alpha",
		Type: ir.TypeRef{Target: "t/x/A"}}
	elsewhere := base
	elsewhere.Provenance = ir.Provenance{Source: ir.NoSource, Pointer: "/paths/~1b/get"}
	elsewhere.Required = true
	elsewhere.Docs = ir.Docs{Summary: "copied into a second position"}

	doc := &ir.Document{Types: ir.TypeRegistry{
		"t/x/A": modelWithProp("t/x/A", base),
		"t/x/B": modelWithProp("t/x/B", elsewhere),
	}}

	assert.Empty(t, duplicateViolations(doc))
}

// TestVerify_ReportsDuplicateIDs pins that Verify runs the check, not just the
// test: an ambiguous identity has to reach a caller that only ever calls Verify.
func TestVerify_ReportsDuplicateIDs(t *testing.T) {
	doc := docWithOperations(ir.Operation{ID: "op/x"}, ir.Operation{ID: "op/x"})

	codes := make([]string, 0, 4)
	for _, v := range Verify(doc) {
		codes = append(codes, v.Code)
	}
	assert.Contains(t, codes, "ir/duplicate-op-id")
}

// identityClasses classifies every named string type the ir package declares by
// whether it is an identity — a class of ID that references resolve against —
// and, where it is, by what resolves those references and what holds the class to
// being declared once.
//
// Both checkers reach a reference by its Go type, so a class nothing resolves
// against goes unchecked in silence rather than failing: that is how every OpID
// and ServiceID reference in the IR went unchecked entirely (GitHub #50). Nothing
// derives the classification. An ID-keyed map on Document is recognizable from
// Document's own shape, but a class with no map is indistinguishable from a class
// nobody has got to yet, and a named string type that is an enum is
// indistinguishable from one that is an identity. This is where that judgement is
// written down, and the test below fails when ir grows a named string type it
// does not account for.
var identityClasses = map[string]string{
	"TypeID":    "identity: Document.Types keys it; checkReferentialIntegrity resolves references, checkRegistryKeys holds each key to its node's own ID and checkDuplicateIDs holds no two nodes to one ID",
	"ChannelID": "identity: Document.Channels keys it; resolved and held as TypeID is",
	"MessageID": "identity: Document.Messages keys it; resolved and held as TypeID is",
	"AuthID":    "identity: Document.Auth keys it; resolved and held as TypeID is",
	"OpID":      "identity, no map: ir.Registries.WithDeclarations resolves references against the operations the document declares, checkDuplicateIDs holds them unique",
	"ServiceID": "identity, no map: resolved and held as OpID is, against the services the document declares",
	"PropID":    "identity, model-scoped: pass.Validate resolves references (checkPropIDRefs, checkEncodingKeys); checkDuplicateIDs holds no two *different* properties to one ID, the copies a component makes of one property being exempt by fingerprint",

	"BigVal":          "arbitrary-precision decimal, not an identity",
	"PrimKind":        "primitive leaf kind; ir.PrimTypeID derives an ID from it, but the kind is not one",
	"TypeKind":        "sum-type tag on TypeDef, not an identity",
	"ValueKind":       "sum-type tag on Value, not an identity",
	"AuthKind":        "auth scheme class, not an identity",
	"AdditionalMode":  "open/closed/constrained additional-property mode, not an identity",
	"PresenceKind":    "wire-presence discipline, not an identity",
	"Lifecycle":       "visibility lifecycle class; an alias of string, so no reflect.Type of its own",
	"StreamingMode":   "streaming direction, not an identity",
	"IdempotencyKind": "idempotency class, not an identity",
	"PageStrategy":    "pagination mechanism, not an identity",
	"HTTPLocation":    "HTTP wire location of a parameter, not an identity",
	"MsgDirection":    "send/receive perspective, not an identity",
	"UnmodeledReason": "why an Unmodeled entry was kept, not an identity",
	"Severity":        "diagnostic severity, not an identity",
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

// declaredStringTypes returns the name of every type the ir package declares as a
// string, defined or aliased.
func declaredStringTypes(t *testing.T) []string {
	t.Helper()
	var names []string
	for name, expr := range typeDecls(t) {
		ident, isIdent := expr.(*ast.Ident)
		if isIdent && ident.Name == "string" {
			names = append(names, name)
		}
	}
	return names
}
