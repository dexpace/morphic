package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// checkMaps asserts no model in the type registry is a map and nothing else
// (ir-design §4.6). Such a shape is a MapT whichever format spelled it, so a
// compiler keeping one a Model gives one construct two shapes, and every
// emitter would have to recognize the map itself. ir.AsMap states the rule, and
// it is the same reading a compiler converts by, so the two cannot disagree.
func checkMaps(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		m, isModel := td.(*ir.Model)
		if !isModel {
			continue // a nil entry is checkRegistryKeys's to report
		}
		if _, isMap := ir.AsMap(m); !isMap {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/map-as-model",
			Message: "model is a map and nothing else, which is a MapT",
			Path:    "types[" + string(id) + "]",
		})
	}
	return vs
}
