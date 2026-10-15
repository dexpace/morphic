package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestCheckMaps_ReportsAModelThatIsAMap pins ir/map-as-model: a model whose
// one member fact is a catch-all is reported, once, at its registry entry,
// while the MapT it should be and a model with anything beside its catch-all
// are not.
func TestCheckMaps_ReportsAModelThatIsAMap(t *testing.T) {
	t.Parallel()
	str := ir.TypeRef{Target: "t/prim/string"}
	for _, tc := range []struct {
		name string
		td   ir.TypeDef
		want []string
	}{
		{"a model that is a map", &ir.Model{AdditionalProps: &ir.AdditionalProps{Value: str}},
			[]string{"types[t/x]"}},
		{"the map it should be", &ir.MapT{Value: str}, nil},
		{"a model with a property beside its catch-all", &ir.Model{
			Properties:      []ir.Property{{ID: "p/x/a", Name: ir.Naming{Source: "a"}, WireName: "a", Type: str}},
			AdditionalProps: &ir.AdditionalProps{Value: str},
		}, nil},
		{"a model with no catch-all", &ir.Model{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.td.Common().ID = "t/x"
			doc := &ir.Document{Types: ir.TypeRegistry{"t/x": tc.td}}

			assert.Equal(t, tc.want, pathsOf(irverify.Verify(doc), "ir/map-as-model"))
		})
	}
}

// pathsOf returns the Path of each violation in vs carrying code, in order.
func pathsOf(vs []irverify.Violation, code string) []string {
	var out []string
	for _, v := range vs {
		if v.Code == code {
			out = append(out, v.Path)
		}
	}
	return out
}
