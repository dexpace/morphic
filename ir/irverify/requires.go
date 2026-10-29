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
	// Every model is read through these two, so a model costs no map of its
	// own however many properties and entries it has.
	var owned reusedMap[string, bool]
	var listed reusedMap[string, int]
	for id, td := range doc.Types {
		m, isModel := td.(*ir.Model)
		if !isModel || m == nil || len(m.Requires) == 0 {
			continue // a nil entry is checkRegistryKeys's to report
		}
		own := owned.emptied()
		for _, p := range m.Properties {
			own[p.EffectiveWireName()] = true
		}
		first := listed.emptied()
		for i, name := range m.Requires {
			if own[name] {
				vs = append(vs, Violation{
					Code:    "ir/requires-own-property",
					Message: "requires " + strconv.Quote(name) + ", which an own property declares and holds as Required",
					Path:    requiresPath(id, i),
				})
			}
			if earlier, repeated := first[name]; repeated {
				vs = append(vs, Violation{
					Code:    "ir/requires-duplicate",
					Message: "requires " + strconv.Quote(name) + " here and at index " + strconv.Itoa(earlier),
					Path:    requiresPath(id, i),
				})
				continue
			}
			first[name] = i
		}
	}
	return vs
}

// requiresPath spells where entry i of the Requires of the model id sits, for
// a violation to be placed there.
func requiresPath(id ir.TypeID, i int) string {
	return typePath(id) + ".Requires[" + strconv.Itoa(i) + "]"
}
