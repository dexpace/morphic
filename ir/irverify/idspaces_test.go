package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// spacedClass is one class of ID with a kind prefix, with a document holding one
// such node under id and the path it is reported at. Unlike declaredIDClass it
// includes the two classes a registry holds, whose path carries the ID.
type spacedClass struct {
	name   string
	prefix string
	doc    func(id string) *ir.Document
	path   func(id string) string
}

func spacedClasses() []spacedClass {
	classes := make([]spacedClass, 0, 6)
	for _, class := range declaredIDClasses() {
		classes = append(classes, spacedClass{
			name:   class.name,
			prefix: class.prefix,
			doc:    func(id string) *ir.Document { return class.doc(id, "") },
			path:   func(string) string { return class.path },
		})
	}
	return append(classes,
		spacedClass{
			name: "type", prefix: ir.IDKindType,
			doc: func(id string) *ir.Document {
				m := &ir.Model{ID: ir.TypeID(id), Name: ir.Naming{Source: "M", Canonical: "m"}}
				return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{m.ID: m}}
			},
			path: func(id string) string { return "doc.Types[" + id + "]" },
		},
		spacedClass{
			name: "auth", prefix: ir.IDKindAuth,
			doc: func(id string) *ir.Document {
				scheme := ir.AuthScheme{ID: ir.AuthID(id), Name: ir.Naming{Source: "a", Canonical: "a"}}
				return &ir.Document{IRVersion: ir.IRVersion, Auth: map[ir.AuthID]ir.AuthScheme{scheme.ID: scheme}}
			},
			path: func(id string) string { return "doc.Auth[" + id + "]" },
		},
	)
}

// declaring returns doc with the given declaration.
func declaring(doc *ir.Document, declaration map[string][]string) *ir.Document {
	doc.IDSpaces = declaration
	return doc
}

// declaredAround returns a declaration under which only the kind of class is
// restricted, to own. The fixtures hold scaffolding IDs of other kinds in the
// namespace "x", and declaring those keeps them out of what a test asserts.
func declaredAround(class spacedClass, own ...string) map[string][]string {
	declaration := map[string][]string{}
	for _, kind := range ir.IDKinds() {
		declaration[kind] = []string{"x"}
	}
	declaration[class.prefix] = own
	return declaration
}

// TestVerify_IDInAnUndeclaredNamespaceIsAViolation is the case the declaration
// exists for: an ID that lost the separator before its path is well-formed, in a
// namespace nobody declared. Every class is tried, because the classes without a
// recorded pointer to agree with are the ones nothing else could catch. The
// control row is the same ID with the separator, which must stay clean.
func TestVerify_IDInAnUndeclaredNamespaceIsAViolation(t *testing.T) {
	t.Parallel()
	for _, class := range spacedClasses() {
		declared := declaredAround(class, "openapi")
		t.Run(class.name+"/separator lost", func(t *testing.T) {
			t.Parallel()
			id := class.prefix + "/openapipaths/x/y"
			got := irverify.Verify(declaring(class.doc(id), declared))
			assert.Equal(t, []string{class.path(id)}, violationPaths(got, "ir/id-space-undeclared"))
			assert.Empty(t, violationPaths(got, "ir/id-malformed"),
				"the point is that shape alone cannot tell: the ID is well-formed")
		})
		t.Run(class.name+"/separator kept", func(t *testing.T) {
			t.Parallel()
			id := class.prefix + "/openapi/paths/x/y"
			got := irverify.Verify(declaring(class.doc(id), declared))
			assert.Empty(t, violationPaths(got, "ir/id-space-undeclared"))
			assert.Empty(t, violationPaths(got, "ir/id-spaces-absent"))
		})
	}
}

// TestVerify_NamespaceDeclaredForAnotherKindDoesNotCount pins that the
// declaration is per kind. A namespace some other kind declares is not one this
// kind may use, which a flat set could not tell.
func TestVerify_NamespaceDeclaredForAnotherKindDoesNotCount(t *testing.T) {
	t.Parallel()
	for _, class := range spacedClasses() {
		t.Run(class.name, func(t *testing.T) {
			t.Parallel()
			other := ir.IDKindGroup
			if class.prefix == other {
				other = ir.IDKindOp
			}
			id := class.prefix + "/openapi/x"
			declared := declaredAround(class, "elsewhere")
			declared[other] = []string{"openapi", "x"}
			got := irverify.Verify(declaring(class.doc(id), declared))
			assert.Equal(t, []string{class.path(id)}, violationPaths(got, "ir/id-space-undeclared"))
		})
	}
}

// TestVerify_PrimitiveNamespaceNeedsNoDeclaration pins the one namespace ir owns:
// the shared primitive leaves are in it in every document, so no producer should
// have to list it, and a document of nothing else needs no declaration at all.
func TestVerify_PrimitiveNamespaceNeedsNoDeclaration(t *testing.T) {
	t.Parallel()
	prim := &ir.Primitive{ID: ir.PrimTypeID(ir.PrimString), Prim: ir.PrimString}
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{prim.ID: prim}}

	got := irverify.Verify(doc)
	assert.Empty(t, violationPaths(got, "ir/id-space-undeclared"))
	assert.Empty(t, violationPaths(got, "ir/id-spaces-absent"))

	declared := declaring(&ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{prim.ID: prim}},
		map[string][]string{ir.IDKindType: {"openapi"}})
	assert.Empty(t, violationPaths(irverify.Verify(declared), "ir/id-space-undeclared"))
}

// TestVerify_PrimitiveNamespaceIsExemptOnlyForTypes pins the exemption's reach.
// The primitive leaves are types, so only a type may live in the namespace ir
// owns without declaring it; an ID of any other kind there is in a namespace
// nobody declared, as it would be anywhere else.
func TestVerify_PrimitiveNamespaceIsExemptOnlyForTypes(t *testing.T) {
	t.Parallel()
	for _, class := range spacedClasses() {
		if class.prefix == ir.IDKindType {
			continue
		}
		t.Run(class.name, func(t *testing.T) {
			t.Parallel()
			id := class.prefix + "/" + ir.IDSpacePrim + "/x"
			got := irverify.Verify(declaring(class.doc(id), declaredAround(class, "openapi")))
			assert.Equal(t, []string{class.path(id)}, violationPaths(got, "ir/id-space-undeclared"))
		})
	}
}

// TestVerify_DocumentDeclaringNothingIsReportedOnce pins how a forgotten
// declaration reads. It is one violation about the document, the way a missing
// irVersion is, and not one per ID it leaves undeclared. A document with no ID
// that needs a namespace has nothing to declare.
func TestVerify_DocumentDeclaringNothingIsReportedOnce(t *testing.T) {
	t.Parallel()
	for _, class := range spacedClasses() {
		t.Run(class.name, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(class.doc(class.prefix + "/openapi/x"))
			assert.Equal(t, []string{"doc.IDSpaces"}, violationPaths(got, "ir/id-spaces-absent"))
			assert.Empty(t, violationPaths(got, "ir/id-space-undeclared"),
				"each ID would be undeclared in an empty vocabulary; that is one defect, not many")
		})
	}
	nothingToDeclare := &ir.Document{IRVersion: ir.IRVersion, Channels: map[ir.ChannelID]ir.Channel{
		"c/x/C": {ID: "c/x/C", Name: ir.Naming{Source: "c", Canonical: "c"}},
	}}
	assert.Empty(t, violationPaths(irverify.Verify(nothingToDeclare), "ir/id-spaces-absent"),
		"channels and messages have no kind prefix, so they need no namespace")
}

// TestVerify_MalformedIDIsNotAlsoUndeclared pins the division of labour: an ID
// too malformed to have a namespace is the shape check's, and reporting it here
// as well would give one defect two reports.
func TestVerify_MalformedIDIsNotAlsoUndeclared(t *testing.T) {
	t.Parallel()
	for _, class := range spacedClasses() {
		t.Run(class.name, func(t *testing.T) {
			t.Parallel()
			id := class.prefix + "//x"
			got := irverify.Verify(declaring(class.doc(id), declaredAround(class, "openapi")))
			assert.Empty(t, violationPaths(got, "ir/id-space-undeclared"))
			assert.NotEmpty(t, violationPaths(got, "ir/id-malformed"), "the malformed ID is still reported")
		})
	}
}

// TestVerify_DeclaredNamespaceNoIDUsesIsClean pins that the declaration is a
// vocabulary and not a usage report. A document without webhooks still lists the
// namespace its compiler would mint them in; whether a compiler's list matches
// what its corpus really uses is that compiler's test.
func TestVerify_DeclaredNamespaceNoIDUsesIsClean(t *testing.T) {
	t.Parallel()
	class := spacedClasses()[0]
	doc := declaring(class.doc(class.prefix+"/openapi/x"), declaredAround(class, "never-used", "openapi"))
	for _, code := range []string{"ir/id-space-undeclared", "ir/id-spaces-absent", "ir/id-space-invalid",
		"ir/id-spaces-unknown-kind", "ir/id-spaces-not-canonical"} {
		assert.Empty(t, violationPaths(irverify.Verify(doc), code), code)
	}
}

// TestVerify_UnusableDeclarationIsAViolation holds the declaration itself to
// what a reader relies on: its keys are kind prefixes, each namespace could be
// one, and each list is in the canonical form two producers of one vocabulary
// share. Each row breaks exactly one of those.
func TestVerify_UnusableDeclarationIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		declaration map[string][]string
		code        string
		path        string
	}{
		{"a key that is no kind prefix", map[string][]string{"x": {"a"}}, "ir/id-spaces-unknown-kind", `doc.IDSpaces["x"]`},
		{"an empty key", map[string][]string{"": {"a"}}, "ir/id-spaces-unknown-kind", `doc.IDSpaces[""]`},
		{"an empty namespace", map[string][]string{ir.IDKindOp: {""}}, "ir/id-space-invalid", "doc.IDSpaces[op][0]"},
		{"a namespace with the separator", map[string][]string{ir.IDKindOp: {"a/b"}}, "ir/id-space-invalid", "doc.IDSpaces[op][0]"},
		{"a kind with an empty list", map[string][]string{ir.IDKindOp: {}}, "ir/id-spaces-not-canonical", "doc.IDSpaces[op]"},
		{"a kind with a nil list", map[string][]string{ir.IDKindOp: nil}, "ir/id-spaces-not-canonical", "doc.IDSpaces[op]"},
		{"an unsorted list", map[string][]string{ir.IDKindOp: {"b", "a"}}, "ir/id-spaces-not-canonical", "doc.IDSpaces[op][1]"},
		{"a repeated namespace", map[string][]string{ir.IDKindOp: {"a", "a"}}, "ir/id-spaces-not-canonical", "doc.IDSpaces[op][1]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := irverify.Verify(&ir.Document{IRVersion: ir.IRVersion, IDSpaces: tc.declaration})
			assert.Equal(t, []string{tc.path}, violationPaths(got, tc.code))
		})
	}
}

// TestVerify_ListOrderIsTheByteOrderOfUTF8 pins which order "sorted" means. A
// producer sorting by UTF-16 code units, as JavaScript and Java do by default,
// puts a character outside the BMP before U+FF5E; the byte order of the UTF-8
// spelling, which the canonical form names, puts it after.
func TestVerify_ListOrderIsTheByteOrderOfUTF8(t *testing.T) {
	t.Parallel()
	bmp, astral := string(rune(0xFF5E)), string(rune(0x1F600))
	verify := func(spaces ...string) []irverify.Violation {
		return irverify.Verify(&ir.Document{IRVersion: ir.IRVersion, IDSpaces: map[string][]string{ir.IDKindOp: spaces}})
	}
	assert.Empty(t, violationPaths(verify(bmp, astral), "ir/id-spaces-not-canonical"))
	assert.Equal(t, []string{"doc.IDSpaces[op][1]"}, violationPaths(verify(astral, bmp), "ir/id-spaces-not-canonical"))
}

// TestVerify_UnknownKindIsNotAlsoExamined pins the division of labour inside the
// declaration: a key that is no kind prefix is reported once, as that, and its
// list is not judged as a kind's would be, so one defect gets one report.
func TestVerify_UnknownKindIsNotAlsoExamined(t *testing.T) {
	t.Parallel()
	got := irverify.Verify(&ir.Document{
		IRVersion: ir.IRVersion,
		IDSpaces:  map[string][]string{"x": {"b", "a", ""}},
	})
	assert.Equal(t, []string{`doc.IDSpaces["x"]`}, violationPaths(got, "ir/id-spaces-unknown-kind"))
	assert.Empty(t, violationPaths(got, "ir/id-spaces-not-canonical"))
	assert.Empty(t, violationPaths(got, "ir/id-space-invalid"))
}

// TestVerify_UsableDeclarationIsClean is the control for the table above: a
// declaration naming every kind, sorted, with namespaces of the shapes the
// compilers use, reports nothing about itself.
func TestVerify_UsableDeclarationIsClean(t *testing.T) {
	t.Parallel()
	declaration := map[string][]string{}
	for _, kind := range ir.IDKinds() {
		declaration[kind] = []string{"anon", "composed", "openapi", "path-prefix"}
	}
	got := irverify.Verify(&ir.Document{IRVersion: ir.IRVersion, IDSpaces: declaration})
	assert.Empty(t, got)
}
