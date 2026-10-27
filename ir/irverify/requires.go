package irverify

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkRequires asserts each model's Requires names a member once and names
// none of its own properties (ir-design §4.3). A name an own property declares
// is that property's Required, so listing it too gives one fact two homes, and
// a repeat admits nothing more. A repeat is reported at its later entry.
func checkRequires(doc *ir.Document) []Violation {
	var vs []Violation
	for id, td := range doc.Types {
		m, isModel := td.(*ir.Model)
		if !isModel || m == nil || len(m.Requires) == 0 {
			continue // a nil entry is checkRegistryKeys's to report
		}
		own := make(map[string]bool, len(m.Properties))
		for _, p := range m.Properties {
			own[p.EffectiveWireName()] = true
		}
		first := make(map[string]int, len(m.Requires))
		for i, name := range m.Requires {
			at := "types[" + string(id) + "].Requires[" + strconv.Itoa(i) + "]"
			if own[name] {
				vs = append(vs, Violation{
					Code:    "ir/requires-own-property",
					Message: "requires " + strconv.Quote(name) + ", which an own property declares and holds as Required",
					Path:    at,
				})
			}
			if earlier, repeated := first[name]; repeated {
				vs = append(vs, Violation{
					Code:    "ir/requires-duplicate",
					Message: "requires " + strconv.Quote(name) + " here and at index " + strconv.Itoa(earlier),
					Path:    at,
				})
				continue
			}
			first[name] = i
		}
	}
	return vs
}
