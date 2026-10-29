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
			Path:    typePath(id),
		})
	}
	return vs
}

// checkMapKeys asserts no map or catch-all names the string primitive as its
// key. A nil Key is how both state string keys (ir-design §4.6), and naming the
// primitive spells that one key type a second way, which every consumer
// comparing keys would have to know. A nullable string key is another type and
// stays. checkPrimIDs holds the primitive to its one ID, so the ID decides.
func checkMapKeys(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		key, field := catchAllKey(td)
		if key == nil || key.Nullable || key.Target != ir.PrimTypeID(ir.PrimString) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/map-key-string",
			Message: "key names the string primitive, which a nil key already states",
			Path:    typePath(id) + "." + field,
		})
	}
	return vs
}

// catchAllKey returns the key td types its members under and the field
// holding it, or nil when td types none: a MapT's Key, or the Key of a Model's
// AdditionalProps.
func catchAllKey(td ir.TypeDef) (*ir.TypeRef, string) {
	switch n := td.(type) {
	case *ir.MapT:
		if n != nil {
			return n.Key, "Key"
		}
	case *ir.Model:
		if n != nil && n.AdditionalProps != nil {
			return n.AdditionalProps.Key, "AdditionalProps.Key"
		}
	default:
		// No other kind types its members under a key.
	}
	return nil, ""
}
