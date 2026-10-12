package irverify_test

import (
	"encoding/json/jsontext"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestVerify_MalformedTypeIDIsAViolation plants IDs the grammar could not have
// produced. Each is a way a derivation can go wrong while the document still
// looks well-formed everywhere else: the registry key matches the node, every
// reference resolves, and nothing else in Verify has an opinion.
func TestVerify_MalformedTypeIDIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.TypeID
	}{
		{name: "no kind prefix", id: "x/space/path"},
		{name: "kind alone", id: "t"},
		{name: "kind with a trailing separator and no space", id: "t/"},
		{name: "empty space", id: "t//path"},
		{name: "empty path", id: "t/space/"},
		{name: "another kind's prefix", id: "op/space/path"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &ir.Model{
				ID: tc.id, Name: ir.Naming{Source: "M", Canonical: "m"},
			}
			doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{tc.id: m}}
			assert.Contains(t, violationCodes(verifyDeclared(doc)), "ir/id-malformed",
				"%q is not an ID the grammar produces", tc.id)
		})
	}
}

// TestVerify_IDDisagreeingWithItsPointerIsAViolation is the half that catches
// GitHub #141's actual instance. "t/anonaddr" lost the separator between its
// space and its path, so by shape it reads as a legitimate ID in a space named
// "anonaddr" — indistinguishable from a real one. What gives it away is the
// source pointer recorded beside it, which the path no longer matches.
func TestVerify_IDDisagreeingWithItsPointerIsAViolation(t *testing.T) {
	t.Parallel()
	m := &ir.Model{
		ID:         "t/anonaddr",
		Name:       ir.Naming{Source: "Addr", Canonical: "addr"},
		Provenance: ir.Provenance{Pointer: "addr"},
	}
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}

	got := verifyDeclared(doc)
	assert.Contains(t, violationCodes(got), "ir/id-provenance-disagreement")
	assert.NotContains(t, violationCodes(got), "ir/id-malformed",
		"the point of this case is that shape alone cannot tell: it is well-shaped")
}

// TestVerify_WrongPointerIsAViolation covers the other direction — a
// well-shaped ID whose recorded pointer names a different coordinate than the
// one it was built from. That is the defect a signature change introduces when
// it passes the wrong `at`, which nothing else in the repository can see.
func TestVerify_WrongPointerIsAViolation(t *testing.T) {
	t.Parallel()
	m := &ir.Model{
		ID:         "t/openapi/components/schemas/Child",
		Name:       ir.Naming{Source: "Child", Canonical: "child"},
		Provenance: ir.Provenance{Pointer: "/components/schemas/Parent"},
	}
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
	assert.Contains(t, violationCodes(verifyDeclared(doc)), "ir/id-provenance-disagreement")
}

// TestVerify_PointerlessIDIsClean pins the exclusion: a primitive is shared
// across every source position and derives from none, so it records no pointer
// and is held to no agreement with one. It is held to the ID its kind derives
// instead, which the cases below cover; requiring a pointer agreement it cannot
// have would make every document violate.
func TestVerify_PointerlessIDIsClean(t *testing.T) {
	t.Parallel()
	p := &ir.Primitive{ID: "t/prim/string", Prim: ir.PrimString}
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{p.ID: p}}
	assert.Empty(t, verifyDeclared(doc))
}

// TestVerify_PrimitiveAwayFromItsSharedIDIsAViolation plants primitive IDs no
// other check judges: each is well-shaped, keyed by its own node ID, and
// records no pointer to disagree with (GitHub #73).
//
// The rows break the agreement two ways. The first two put a shared leaf in a
// format's own space, so one type lowered from two formats stops being one
// type; the second looks right, a private space merely spelled "prim". The rest
// keep the shared space with a path that is not the node's kind (another kind,
// none, or re-cased), which contradicts itself: no consumer can tell which to
// believe.
func TestVerify_PrimitiveAwayFromItsSharedIDIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.TypeID
		kind ir.PrimKind
	}{
		{name: "a per-position ID", id: "t/openapi/components/schemas/Name", kind: ir.PrimString},
		{name: "a compiler's private prim space", id: "t/graphql/prim/string", kind: ir.PrimString},
		{name: "the right space, the wrong kind", id: "t/prim/int32", kind: ir.PrimString},
		{name: "the space alone", id: "t/prim", kind: ir.PrimString},
		{name: "a re-cased kind", id: "t/prim/dateTimeOffset", kind: ir.PrimDatetimeOffset},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &ir.Primitive{ID: tc.id, Prim: tc.kind}
			doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{tc.id: p}}

			got := verifyDeclared(doc)
			assert.Contains(t, violationCodes(got), "ir/prim-id-not-derived")
			assert.NotContains(t, violationCodes(got), "ir/id-malformed",
				"the point of these cases is that shape alone cannot tell: each is well-shaped")
		})
	}
}

// TestVerify_KindlessPrimitiveIsReportedOnItsOwnTerms pins the one case with no
// destination to name. ir.PrimTypeID derives t/prim/ from the zero-value kind,
// which is not an ID at all, so the message must not offer it as the place the
// node belongs — a reader sent there fixes the wrong end, and the check would be
// telling them to write an ID checkIDs reports as malformed.
//
// The kindless primitive breaks two claims at once and each is stated: the ID is
// not the one its kind derives, and the kind is not one ir declares. They name
// different repairs, so neither subsumes the other.
func TestVerify_KindlessPrimitiveIsReportedOnItsOwnTerms(t *testing.T) {
	t.Parallel()
	const id ir.TypeID = "t/openapi/components/schemas/Name"
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{
		id: &ir.Primitive{ID: id},
	}}

	got := verifyDeclared(doc)
	require.Len(t, got, 2)
	assert.Equal(t, "ir/prim-id-not-derived", got[0].Code)
	assert.Contains(t, got[0].Message, "carries no kind")
	assert.NotContains(t, got[0].Message, string(ir.PrimTypeID("")),
		"t/prim/ is not an ID; naming it as the destination sends the reader to the wrong end")
	assert.Equal(t, "ir/unknown-prim-kind", got[1].Code)
}

// TestVerify_NonPrimitiveInThePrimSpaceIsAViolation covers the other direction.
// The space is reserved rather than conventional: a node there either collides
// with the primitive of that kind outright, or squats a name the next PrimKind
// takes — and either way which node is reached depends on what was interned
// first, which is invariant 3's corollary.
func TestVerify_NonPrimitiveInThePrimSpaceIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.TypeID
	}{
		{name: "squatting an existing kind", id: "t/prim/string"},
		{name: "squatting a name no kind uses yet", id: "t/prim/instant"},
		{name: "the space alone", id: "t/prim"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &ir.Model{
				ID: tc.id, Name: ir.Naming{Source: "M", Canonical: "m"},
			}
			doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{tc.id: m}}
			assert.Contains(t, violationCodes(verifyDeclared(doc)), "ir/prim-space-reserved")
		})
	}
}

// TestVerify_PrimIDChecksAreScopedToTheSpaceAndTheKind is the control for both
// checks above: every primitive at the ID its kind derives, beside ordinary
// types in a format's own space, reports nothing. Without it a check that fired
// on everything would pass both tables.
//
// "prim" appears in one model as a path segment and in the other inside a name,
// and neither is in the reserved space, which only reading the space segment
// can tell. Matching the ID as a substring passes both tables above and fails
// here.
func TestVerify_PrimIDChecksAreScopedToTheSpaceAndTheKind(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{}}
	for _, kind := range []ir.PrimKind{ir.PrimString, ir.PrimInt32, ir.PrimDatetimeOffset, ir.PrimAny} {
		id := ir.PrimTypeID(kind)
		doc.Types[id] = &ir.Primitive{ID: id, Prim: kind}
	}
	for _, m := range []struct {
		id      ir.TypeID
		pointer jsontext.Pointer
		source  string
	}{
		{id: "t/openapi/prim/string", pointer: "/prim/string", source: "String"},
		{id: "t/openapi/components/schemas/primitive", pointer: "/components/schemas/primitive", source: "primitive"},
	} {
		doc.Types[m.id] = &ir.Model{
			ID:         m.id,
			Name:       ir.Naming{Source: m.source, Canonical: ir.CanonicalWords(m.source)},
			Provenance: ir.Provenance{Pointer: m.pointer},
		}
	}

	assert.Empty(t, verifyDeclared(doc),
		"an ID carrying \"prim\" outside the space segment is not in the reserved space")
}

// TestVerify_AuthIDIsHeldToTheSameRule pins that the check is about the grammar
// rather than about one registry: an auth scheme's ID is derived the same way and
// is held to the same agreement.
func TestVerify_AuthIDIsHeldToTheSameRule(t *testing.T) {
	t.Parallel()
	scheme := ir.AuthScheme{
		ID:         "auth/openapi/components/securitySchemes/Other",
		Name:       ir.Naming{Source: "apiKey", Canonical: "api_key"},
		Provenance: ir.Provenance{Pointer: "/components/securitySchemes/apiKey"},
	}
	doc := &ir.Document{IRVersion: ir.IRVersion, Auth: map[ir.AuthID]ir.AuthScheme{scheme.ID: scheme}}
	assert.Contains(t, violationCodes(verifyDeclared(doc)), "ir/id-provenance-disagreement")
}

// TestVerify_DerivedIDsAreClean is the control for every case above: an ID whose
// path is exactly the pointer it records reports nothing, at both the
// pointer-derived spaces a compiler addresses and a minted one.
func TestVerify_DerivedIDsAreClean(t *testing.T) {
	t.Parallel()
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{}}
	for _, id := range []ir.TypeID{
		"t/openapi/components/schemas/User",
		"t/anon/paths/~1pets/get/responses/200/content/application~1json/schema",
		"t/composed/components/schemas/Combo/oneOf/0",
	} {
		path, ok := ir.IDPath(ir.IDKindType, string(id))
		require.True(t, ok, "%s carries a path", id)
		doc.Types[id] = &ir.Model{
			ID:         id,
			Name:       ir.Naming{Source: "N", Canonical: "n"},
			Provenance: ir.Provenance{Pointer: jsontext.Pointer("/" + path)},
		}
	}
	assert.Empty(t, verifyDeclared(doc))
}

// declaredIDClass is one class of ID a node declares for itself, with a document
// holding exactly one such node and the path it is reported at. The documents
// are not otherwise valid; every assertion below filters by code.
type declaredIDClass struct {
	name   string
	noun   string // the word its violation codes spell the class with, as in ir/empty-<noun>-id
	prefix string
	path   string
	doc    func(id, pointer string) *ir.Document
}

func declaredIDClasses() []declaredIDClass {
	service := func(groups ...ir.OperationGroup) *ir.Document {
		return &ir.Document{IRVersion: ir.IRVersion, Services: []ir.Service{{ID: "s/x", Groups: groups}}}
	}
	return []declaredIDClass{
		{
			name: "operation", noun: "op", prefix: ir.IDKindOp, path: "doc.Services[0].Groups[0].Operations[0]",
			doc: func(id, pointer string) *ir.Document {
				return service(ir.OperationGroup{ID: "g/x", Operations: []ir.Operation{{
					ID: ir.OpID(id), Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)},
				}}})
			},
		},
		{
			name: "service", noun: "service", prefix: ir.IDKindService, path: "doc.Services[0]",
			doc: func(id, pointer string) *ir.Document {
				doc := service()
				doc.Services[0].ID = ir.ServiceID(id)
				doc.Services[0].Provenance.Pointer = jsontext.Pointer(pointer)
				return doc
			},
		},
		{
			name: "group", noun: "group", prefix: ir.IDKindGroup, path: "doc.Services[0].Groups[0]",
			doc: func(id, _ string) *ir.Document { return service(ir.OperationGroup{ID: ir.GroupID(id)}) },
		},
		{
			name: "property", noun: "prop", prefix: ir.IDKindProp, path: "doc.Types[t/x/M].Properties[0]",
			doc: func(id, pointer string) *ir.Document {
				m := &ir.Model{ID: "t/x/M", Properties: []ir.Property{{
					ID: ir.PropID(id), Provenance: ir.Provenance{Pointer: jsontext.Pointer(pointer)},
				}}}
				return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
			},
		},
	}
}

// declaredIDClassNamed returns the class called name, so a test reads the one it
// means however the table is ordered.
func declaredIDClassNamed(t *testing.T, name string) declaredIDClass {
	t.Helper()
	for _, class := range declaredIDClasses() {
		if class.name == name {
			return class
		}
	}
	require.FailNow(t, "no such class of declared ID", name)
	return declaredIDClass{}
}

// violationPaths returns the paths of the violations of one code, in report
// order.
func violationPaths(vs []irverify.Violation, code string) []string {
	var out []string
	for _, v := range vs {
		if v.Code == code {
			out = append(out, v.Path)
		}
	}
	return out
}

// TestVerify_MalformedDeclaredIDIsAViolation holds the classes that have no
// registry key to the grammar checkIDs holds a type to: an operation, a service,
// a group or a property whose ID the grammar could not have produced is
// reported once, at the node. Before this only a type's or a scheme's was, so
// the rest could carry any string and still verify.
//
// Every class is tried against every other class's prefix. A prefix table that
// swapped two classes still accepts each one's own spelling in a one-class test,
// and reddens only here.
func TestVerify_MalformedDeclaredIDIsAViolation(t *testing.T) {
	t.Parallel()
	type row struct{ name, id string }
	for _, class := range declaredIDClasses() {
		malformed := []row{
			{"no kind prefix", "x/space/path"},
			{"a type's prefix", "t/space/path"},
			{"the kind alone", class.prefix},
			{"no space", class.prefix + "/"},
			{"an empty space", class.prefix + "//path"},
			{"an empty path", class.prefix + "/space/"},
			{"a longer word opening with it", class.prefix + "x/space/path"},
		}
		for _, other := range declaredIDClasses() {
			if other.prefix != class.prefix {
				malformed = append(malformed, row{"the " + other.name + " prefix", other.prefix + "/space/path"})
			}
		}
		for _, tc := range malformed {
			t.Run(class.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				got := verifyDeclared(class.doc(tc.id, ""))
				assert.Equal(t, []string{class.path}, violationPaths(got, "ir/id-malformed"),
					"%q is not an ID the %s grammar produces", tc.id, class.name)
			})
		}
	}
}

// TestVerify_WellFormedDeclaredIDIsClean is the control: the spellings the
// compilers mint, a space that names a single node and a path with separators
// of its own report nothing, so a check that fired on everything fails here.
func TestVerify_WellFormedDeclaredIDIsClean(t *testing.T) {
	t.Parallel()
	for _, class := range declaredIDClasses() {
		for _, suffix := range []string{"/openapi/a/b", "/space", "/openapi/~1x/get"} {
			id := class.prefix + suffix
			t.Run(class.name+" "+id, func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, violationPaths(verifyDeclared(class.doc(id, "")), "ir/id-malformed"))
			})
		}
	}
}

// TestVerify_EmptyDeclaredIDIsNotAlsoMalformed pins the division of labour: an
// empty ID is checkDeclaredIDs' to report, under its own code, and reporting it
// here too would give one defect two reports.
func TestVerify_EmptyDeclaredIDIsNotAlsoMalformed(t *testing.T) {
	t.Parallel()
	for _, class := range declaredIDClasses() {
		t.Run(class.name, func(t *testing.T) {
			t.Parallel()
			got := verifyDeclared(class.doc("", ""))
			assert.Empty(t, violationPaths(got, "ir/id-malformed"))
			assert.Equal(t, []string{class.path}, violationPaths(got, "ir/empty-"+class.noun+"-id"),
				"the empty ID is still reported, by the rule that owns it")
		})
	}
}

// TestVerify_PropertyIDDisagreeingWithItsPointerIsAViolation extends #141's
// guard to properties, whose IDs are minted from the pointer they record. The
// first row is #141's own shape: the separator between the space and the path is
// gone, so the ID is well-formed in a space named "openapicomponents", and only
// the pointer beside it gives it away.
func TestVerify_PropertyIDDisagreeingWithItsPointerIsAViolation(t *testing.T) {
	t.Parallel()
	property := declaredIDClassNamed(t, "property")
	tests := []struct{ name, id, pointer string }{
		{"a lost separator", "p/openapicomponents/schemas/M/properties/f", "/components/schemas/M/properties/f"},
		{"another position's pointer", "p/openapi/components/schemas/M/properties/f", "/components/schemas/M/properties/g"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := verifyDeclared(property.doc(tc.id, tc.pointer))
			assert.Equal(t, []string{property.path}, violationPaths(got, "ir/id-provenance-disagreement"))
			assert.Empty(t, violationPaths(got, "ir/id-malformed"),
				"each is well-shaped, which is why shape alone cannot tell")
		})
	}
}

// TestVerify_PropertyIDAgreeingWithItsPointerIsClean is the control: the ID a
// compiler derives from the pointer, and an ID whose node records none, report
// nothing.
func TestVerify_PropertyIDAgreeingWithItsPointerIsClean(t *testing.T) {
	t.Parallel()
	property := declaredIDClassNamed(t, "property")
	const id = "p/openapi/components/schemas/M/properties/f"
	for _, pointer := range []string{"/components/schemas/M/properties/f", ""} {
		got := verifyDeclared(property.doc(id, pointer))
		assert.Empty(t, violationPaths(got, "ir/id-provenance-disagreement"), "pointer %q", pointer)
	}
}

// TestVerify_OperationIDMayDifferFromItsPointer pins the exclusion. An
// operation reached through a $ref'd path item is identified by where it is
// mounted and records where its body is declared (GitHub #107), so the two
// legitimately differ; holding it to the agreement a property keeps would fail
// every document that reuses a path item. Nor is a service, whose ID is a
// source index and not a path.
func TestVerify_OperationIDMayDifferFromItsPointer(t *testing.T) {
	t.Parallel()
	operation, service := declaredIDClassNamed(t, "operation"), declaredIDClassNamed(t, "service")

	mounted := operation.doc("op/openapi/paths/~1widgets/get", "/components/pathItems/Listing/get")
	assert.Empty(t, violationPaths(verifyDeclared(mounted), "ir/id-provenance-disagreement"))
	indexed := service.doc("s/openapi/0", "/info")
	assert.Empty(t, violationPaths(verifyDeclared(indexed), "ir/id-provenance-disagreement"))
}

// violationCodes returns the codes of vs, for set-membership assertions that do
// not depend on violation order or message wording.
func violationCodes(vs []irverify.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Code)
	}
	return out
}
