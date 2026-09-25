package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// leaf is the well-formed scalar every document below is closed over, so a
// reference that does name a target — the tuple's first element, the clean
// model's property — resolves rather than dangling.
func leaf() *ir.Scalar {
	return &ir.Scalar{
		ID:   "t/x/Leaf",
		Name: ir.Naming{Source: "Leaf", Canonical: "leaf"},
	}
}

// closedDoc returns a document holding tds beside the leaf.
func closedDoc(tds ...ir.TypeDef) *ir.Document {
	l := leaf()
	reg := ir.TypeRegistry{l.ID: l}
	for _, td := range tds {
		reg[td.Common().ID] = td
	}
	return &ir.Document{Types: reg}
}

// operationDoc returns a closed document whose one operation takes p. It is
// the fixture outside the type registry: a compiled document holds TypeRefs
// under Services as well as under Types, so a check walking only doc.Types
// would miss those while every registry fixture stayed green.
func operationDoc(p ir.Parameter) *ir.Document {
	doc := closedDoc()
	doc.Services = []ir.Service{{
		ID:   "s/x/S",
		Name: named("S"),
		Groups: []ir.OperationGroup{{Operations: []ir.Operation{{
			ID:     "op/x/S/op",
			Name:   named("op"),
			Params: []ir.Parameter{p},
		}}}},
	}}
	return doc
}

// typeRefViolations returns the ir/type-ref-no-target violations in doc, so a
// test asserting none is not satisfied by an unrelated violation being absent.
func typeRefViolations(t *testing.T, doc *ir.Document) []irverify.Violation {
	t.Helper()
	var out []irverify.Violation
	for _, v := range irverify.Verify(doc) {
		if v.Code == "ir/type-ref-no-target" {
			out = append(out, v)
		}
	}
	return out
}

// TestVerify_UnionVariantWithNoTargetIsAViolation is the reproducer from GitHub
// #397: a union of one variant passes the arity check, and a target of "" is
// skipped by the reference check, so before this check nothing reported it. The
// dangling code is asserted absent to pin that this is a gap between the two
// checks rather than a defect one of them already covers.
func TestVerify_UnionVariantWithNoTargetIsAViolation(t *testing.T) {
	t.Parallel()
	doc := closedDoc(&ir.Union{
		ID:       "t/x/U",
		Name:     ir.Naming{Source: "U", Canonical: "u"},
		Variants: []ir.Variant{{Type: ir.TypeRef{Target: ""}}},
	})

	all := irverify.Verify(doc)
	assert.NotContains(t, codes(all), "ir/dangling-type-ref", "an empty target is not a dangling one")

	got := typeRefViolations(t, doc)
	require.Len(t, got, 1, "the empty target is reported exactly once")
	assert.Equal(t, "doc.Types[t/x/U].Variants[0].Type.Target", got[0].Path,
		"the violation locates the target field, where a dangling one would be reported")
	assert.Contains(t, got[0].Message, "no target")
}

// TestVerify_EmptyTargetIsReportedAtEveryCarrierShape holds the rule to be
// about the TypeRef type rather than the variant position it was noticed at.
// A by-value TypeRef, a slice element, and a non-nil pointer are each reported:
// the IR spells "no type here" as a nil *TypeRef, so a pointer that was
// allocated and then given nothing to name is the same defect as a value. The
// parameter row sits outside doc.Types, pinning the walk to the whole document.
func TestVerify_EmptyTargetIsReportedAtEveryCarrierShape(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		doc  *ir.Document
		path string
	}{
		{
			name: "a property's type",
			doc: closedDoc(&ir.Model{
				ID:   "t/x/M",
				Name: ir.Naming{Source: "M", Canonical: "m"},
				Properties: []ir.Property{{
					ID:   "p/x/M/f",
					Name: ir.Naming{Source: "f", Canonical: "f"},
					Type: ir.TypeRef{},
				}},
			}),
			path: "doc.Types[t/x/M].Properties[0].Type.Target",
		},
		{
			name: "a list's element",
			doc: closedDoc(&ir.List{
				ID:   "t/x/L",
				Name: ir.Naming{Source: "L", Canonical: "l"},
				Elem: ir.TypeRef{},
			}),
			path: "doc.Types[t/x/L].Elem.Target",
		},
		{
			name: "a tuple's element",
			doc: closedDoc(&ir.Tuple{
				ID:    "t/x/T",
				Name:  ir.Naming{Source: "T", Canonical: "t"},
				Elems: []ir.TypeRef{{Target: "t/x/Leaf"}, {}},
			}),
			path: "doc.Types[t/x/T].Elems[1].Target",
		},
		{
			name: "a model's base, allocated but naming nothing",
			doc: closedDoc(&ir.Model{
				ID:   "t/x/M",
				Name: ir.Naming{Source: "M", Canonical: "m"},
				Base: &ir.TypeRef{},
			}),
			path: "doc.Types[t/x/M].Base.Target",
		},
		{
			name: "a parameter's type, outside the type registry",
			doc:  operationDoc(ir.Parameter{Name: named("p"), Type: ir.TypeRef{}}),
			path: "doc.Services[0].Groups[0].Operations[0].Params[0].Type.Target",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := typeRefViolations(t, tc.doc)
			require.Len(t, got, 1)
			assert.Equal(t, tc.path, got[0].Path)
		})
	}
}

// TestVerify_AbsentOptionalTypeRefIsClean pins the other side of the rule: a nil
// *TypeRef is how the IR says a model declares no base, so it is not a reference
// that names nothing and is not reported. The sibling with a target resolves,
// so the document is clean under every check this file is about.
func TestVerify_AbsentOptionalTypeRefIsClean(t *testing.T) {
	t.Parallel()
	doc := closedDoc(&ir.Model{
		ID:   "t/x/M",
		Name: ir.Naming{Source: "M", Canonical: "m"},
		Base: nil,
		Properties: []ir.Property{{
			ID:   "p/x/M/f",
			Name: ir.Naming{Source: "f", Canonical: "f"},
			Type: ir.TypeRef{Target: "t/x/Leaf"},
		}},
	})
	assert.Empty(t, typeRefViolations(t, doc))
}
