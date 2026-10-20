package census

import (
	"maps"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/internal/decls"
)

// MaxTypes is the bound on the types one derivation visits.
const MaxTypes = maxTypes

// Expand is the signature of the hook that says how to cross an interface.
type Expand = expandFunc

// Derive exposes derive, with the hook it is handed.
func Derive(roots []reflect.Type, p *decls.Package, expand Expand) (Universe, error) {
	return derive(roots, p, expand)
}

// Reach returns, sorted, the members of the structs the roots reach.
func Reach(roots []reflect.Type, concrete map[reflect.Type][]reflect.Type) ([]string, error) {
	r, err := reachable(roots, concrete)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ms := range r.structs {
		for _, m := range ms {
			names = append(names, m.name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// Enumerations returns the enumerations the package declares, as lines of the
// type's name and its declared values, each in the order a report lists them.
func Enumerations(p *decls.Package) ([]string, error) {
	enums, err := enumerations(p)
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, e := range enums {
		for _, s := range slices.SortedFunc(maps.Keys(e.declared), e.compareSpellings) {
			lines = append(lines, e.name+" "+e.render(s))
		}
	}
	return lines, nil
}

// ExpandTypeDef runs expandTypeDef over one enumeration named name that
// declares the given values.
func ExpandTypeDef(name string, values ...string) error {
	_, err := expandTypeDef(map[string][]string{name: values})
	return err
}

// Held returns what a value of the numeric enumeration declaring the values
// counts as.
func Held(spelling string, values ...string) []string {
	declared := map[string]bool{}
	for _, v := range values {
		declared[v] = true
	}
	e := &enumeration{name: "E", numeric: true, declared: declared}
	e.readFlags()
	return e.held(spelling)
}

// AddAny counts the facts root holds, whatever its type.
func (c *Census) AddAny(root any) {
	c.add(root)
}
