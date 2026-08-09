package nodeview

import (
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
// components mapping, which is what the reference scan does to a document whose
// every schema is referenced once.
//
// It guards a shape rather than a number. The walk descends the same mapping
// once per reference, so the pairs it reads grow as references × components
// without keyIndex — and those two grow together in a real document, making the
// scan quadratic in the document's own size. Each width here does n times the
// work of a single resolution, so the *per-component* cost is what to read:
// divide by n and compare across widths. It should stay flat, and a run where it
// grows with n is the index no longer being reached.
func BenchmarkPointerPath_IntoAWideMapping(b *testing.B) {
	for _, n := range []int{64, 256, 1024} {
		root := componentsDoc(n)
		pointers := make([]string, n)
		for i := range pointers {
			pointers[i] = fmt.Sprintf("/components/schemas/S%d", i)
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
