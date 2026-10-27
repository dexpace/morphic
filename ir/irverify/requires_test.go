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

// TestVerify_RequiresReadsTheEffectiveWireName holds the own-property rule to
// the name a property travels under: its WireName, or its source name where it
// sets none (ir-design §5.1). A source name a WireName replaces is no member's.
func TestVerify_RequiresReadsTheEffectiveWireName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		wire     string
		requires []string
		own      bool
	}{
		{"no WireName: the source name is the property's", "", []string{"a"}, true},
		{"a WireName is the property's", "wire", []string{"wire"}, true},
		{"a source name a WireName replaces is no member's", "wire", []string{"a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := requiresDoc(tc.requires)
			doc.Types["t/x/M"].(*ir.Model).Properties[0].WireName = tc.wire

			got := requiresViolations(verifyDeclared(doc), "ir/requires-own-property")
			assert.Equal(t, tc.own, len(got) == 1, "violations: %+v", got)
		})
	}
}
