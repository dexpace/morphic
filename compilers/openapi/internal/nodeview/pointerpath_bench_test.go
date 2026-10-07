package nodeview

import (
	"encoding/json/jsontext"
	"fmt"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// componentsDoc builds `{components: {schemas: {S0..Sn-1: {type: object}}}}`,
// the shape every internal $ref in an OpenAPI document points into.
func componentsDoc(n int) *yaml.Node {
	schemas := &yaml.Node{Kind: yaml.MappingNode}
	for i := range n {
		schemas.Content = append(schemas.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("S%d", i)},
			&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "type"},
				{Kind: yaml.ScalarNode, Value: "object"},
			}})
	}
	components := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "schemas"}, schemas,
	}}
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "components"}, components,
	}}
}

// BenchmarkPointerPath_IntoAWideMapping resolves one pointer per component of a
// components mapping, as the reference scan does when every schema is
// referenced once.
//
// It guards a shape rather than a number. Each width does n times one
// resolution's work, so read the per-component cost: it should stay flat across
// widths, and growth with n means the index is no longer reached
// (TestPointerPath_ReachesTheIndexOnAWideMapping asserts that without a
// stopwatch). Without keyIndex the cost is quadratic.
//
// The narrow widths show where an index loses: below minIndexedPairs the walk
// scans, and a regression there means the gate has stopped paying for itself.
func BenchmarkPointerPath_IntoAWideMapping(b *testing.B) {
	for _, n := range []int{2, 8, 64, 256, 1024} {
		root := componentsDoc(n)
		pointers := make([]jsontext.Pointer, n)
		for i := range pointers {
			pointers[i] = jsontext.Pointer(fmt.Sprintf("/components/schemas/S%d", i))
		}

		b.Run(fmt.Sprintf("components%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v := New() // one view per pass: a view never outlives its compile
				for _, p := range pointers {
					if _, complete := v.PointerPath(root, p); !complete {
						b.Fatalf("pointer %s must resolve", p)
					}
				}
			}
		})
	}
}
