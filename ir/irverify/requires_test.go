package irverify_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// requiresDoc is a document whose one model, t/x/M, declares the property "a"
// and requires the names in requires.
func requiresDoc(requires []string) *ir.Document {
	str := &ir.Primitive{ID: ir.PrimTypeID(ir.PrimString), Prim: ir.PrimString}
	m := &ir.Model{
		ID:   "t/x/M",
		Name: named("m"),
		Properties: []ir.Property{{
			ID: "p/x/M/a", Name: named("a"), WireName: "a", Type: ir.TypeRef{Target: str.ID},
		}},
		Requires: requires,
	}
	return &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{str.ID: str, m.ID: m}}
}

// requiresViolations returns the violations in vs carrying code, in Verify's order.
func requiresViolations(vs []irverify.Violation, code string) []irverify.Violation {
	var out []irverify.Violation
	for _, v := range vs {
		if v.Code == code {
			out = append(out, v)
		}
	}
	return out
}

// TestVerify_RequiresHoldsNoOwnPropertyAndNoRepeat pins the two rules on
// Model.Requires (ir-design §4.3): no name is one an own property declares,
// whose Required is that fact's one home, and no name repeats. Each is reported
// at the offending entry, a repeat at its later index naming the earlier. A
// blank name alone is legal, since a member may be named "", and an entry that
// is no model, or is a nil one, is not this check's to judge.
func TestVerify_RequiresHoldsNoOwnPropertyAndNoRepeat(t *testing.T) {
	t.Parallel()
	const at = "types[t/x/M].Requires["
	own := func(i, name string) irverify.Violation {
		return irverify.Violation{
			Code:    "ir/requires-own-property",
			Message: `requires "` + name + `", which an own property declares and holds as Required`,
			Path:    at + i + "]",
		}
	}
	dup := func(i, earlier, name string) irverify.Violation {
		return irverify.Violation{
			Code:    "ir/requires-duplicate",
			Message: `requires "` + name + `" here and at index ` + earlier,
			Path:    at + i + "]",
		}
	}
	for _, tc := range []struct {
		name     string
		requires []string
		wantOwn  []irverify.Violation
		wantDup  []irverify.Violation
	}{
		{name: "a model requiring nothing"},
		{name: "names no own property declares", requires: []string{"id", "extra"}},
		{
			name: "a name an own property declares", requires: []string{"id", "a"},
			wantOwn: []irverify.Violation{own("1", "a")},
		},
		{
			name: "a repeat is reported at the later index", requires: []string{"id", "extra", "id"},
			wantDup: []irverify.Violation{dup("2", "0", "id")},
		},
		{
			name: "every later copy names the first", requires: []string{"id", "id", "id"},
			wantDup: []irverify.Violation{dup("1", "0", "id"), dup("2", "0", "id")},
		},
		{
			name: "an own name repeated draws both reports", requires: []string{"a", "a"},
			wantOwn: []irverify.Violation{own("0", "a"), own("1", "a")},
			wantDup: []irverify.Violation{dup("1", "0", "a")},
		},
		{name: "a blank name alone", requires: []string{""}},
		{
			name: "a blank name repeated", requires: []string{"", "id", ""},
			wantDup: []irverify.Violation{dup("2", "0", "")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := verifyDeclared(requiresDoc(tc.requires))

			assert.Empty(t, cmp.Diff(tc.wantOwn, requiresViolations(got, "ir/requires-own-property")))
			assert.Empty(t, cmp.Diff(tc.wantDup, requiresViolations(got, "ir/requires-duplicate")))
			if tc.wantOwn == nil && tc.wantDup == nil {
				assert.Empty(t, got, "a legal Requires draws no violation from any check")
			}
		})
	}

	for _, tc := range []struct {
		name string
		td   ir.TypeDef
	}{
		{"a type that is no model", &ir.Scalar{ID: "t/x/M", Name: named("m")}},
		{"a nil model", (*ir.Model)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := &ir.Document{Types: ir.TypeRegistry{"t/x/M": tc.td}}

			for _, v := range irverify.Verify(doc) {
				assert.False(t, strings.HasPrefix(v.Code, "ir/requires-"), "unexpected %+v", v)
			}
		})
	}
}

// TestVerify_RequiresIsHeldModelByModel pins that a model's Requires answers to
// its own properties and entries alone: a name another model declares or also
// requires is no defect there. Each of the two models declares what the other
// requires and both require one name, so whichever the check meets first, what
// it read of that model must not linger into the other.
func TestVerify_RequiresIsHeldModelByModel(t *testing.T) {
	t.Parallel()
	str := &ir.Primitive{ID: ir.PrimTypeID(ir.PrimString), Prim: ir.PrimString}
	model := func(id ir.TypeID, own string, requires ...string) *ir.Model {
		return &ir.Model{ID: id, Name: named(own), Requires: requires, Properties: []ir.Property{{
			ID: ir.PropID("p" + string(id[1:]) + "/" + own), Name: named(own), WireName: own,
			Type: ir.TypeRef{Target: str.ID},
		}}}
	}
	a, b := model("t/x/A", "a", "b", "shared"), model("t/x/B", "b", "a", "shared")
	doc := &ir.Document{IRVersion: ir.IRVersion, Types: ir.TypeRegistry{str.ID: str, a.ID: a, b.ID: b}}

	assert.Empty(t, verifyDeclared(doc))
}
