package schema

import (
	"github.com/dexpace/morphic/compilers/openapi/internal/merge"
	"github.com/dexpace/morphic/ir"
)

// requireMembers lands each name of required on m, once m holds every own
// property: as Required on the property of that wire name, never cleared once
// set, and otherwise in m.Requires, once, in the order required names it. A
// name no own property declares is one a Base or Mixin declares, or one only
// the catch-all governs, and JSON Schema requires it all the same (ir-design
// §4.3). Each object lowering calls it once, on a model that lists none yet.
func requireMembers(m *ir.Model, required []string) {
	byWire := merge.WireNameIndex(m.Properties)
	listed := make(map[string]bool, len(required))
	for _, name := range required {
		if i, ok := byWire[name]; ok {
			m.Properties[i].Required = true
			continue
		}
		if !listed[name] {
			listed[name] = true
			m.Requires = append(m.Requires, name)
		}
	}
}
