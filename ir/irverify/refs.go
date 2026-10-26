package irverify

import (
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// refSite is one discovered ID reference: the class that made it a reference,
// its value, and where in the document it sits.
type refSite struct {
	idType reflect.Type
	id     string
	path   string
}

// checkReferentialIntegrity asserts every typed-ID reference, map keys
// included, resolves in its registry, reporting each unresolved one as dangling.
// Most keys are an entry's own ID, but Service.Renames's are references.
//
// The registries come from Document's own shape, so a new one is checked at
// once, under the code pass.Validate gives it. ir.Operation and ir.Service have
// no map, so ir.Registries.WithDeclarations supplies them from the declared
// identities (GitHub #50), unless the walk truncated: a partial walk would report
// a reference to an unreached operation as dangling.
//
// ir.PropID stays out: pass.Validate's checkPropIDRefs holds it to a declared
// property.
func checkReferentialIntegrity(doc *ir.Document) walkCheck {
	refs := newRefsCollector(doc)
	return walkCheck{
		visit: refs.visit,
		report: func(decls declarations) []Violation {
			unresolved := refs.unresolved(decls)
			vs := make([]Violation, 0, len(unresolved))
			for _, s := range unresolved {
				vs = append(vs, Violation{
					Code:    "ir/dangling-" + ir.RefNoun(s.idType) + "-ref",
					Message: "reference " + strconv.Quote(s.id) + " does not resolve in " + refs.label(s.idType),
					Path:    s.path,
				})
			}
			return vs
		},
	}
}

// refsCollector gathers the references a walk reaches that may not resolve.
//
// A reference into a registry Document keys by its class resolves or not as the
// walk reaches it, so only one that does not is kept, path and all. A declared
// class's registry is complete only once the walk is over, so every reference to
// one is kept for unresolved to settle.
//
// An empty ID is skipped because it is no reference: some positions spell
// "none" with one (Discriminator.Default). Whether a position may hold one is
// checkTypeRefs's question for ir.TypeRef and checkEmptyRefs's for the bare ID
// positions (GitHub #473).
type refsCollector struct {
	keyed ir.Registries
	all   ir.Registries
	sites []refSite
}

// newRefsCollector returns a collector of doc's references.
func newRefsCollector(doc *ir.Document) *refsCollector {
	keyed := ir.DocumentRegistries(doc)
	return &refsCollector{keyed: keyed, all: keyed.WithDeclarations(nil)}
}

// visit keeps v if it is a reference that may not resolve.
func (c *refsCollector) visit(v reflect.Value, at ir.WalkPath) bool {
	if v.Kind() != reflect.String || v.String() == "" {
		return true
	}
	t := v.Type()
	if _, isRef := c.all[t]; !isRef {
		return true
	}
	if reg, keyed := c.keyed[t]; keyed && reg.Has(v.String()) {
		return true
	}
	c.sites = append(c.sites, refSite{idType: t, id: v.String(), path: at.String()})
	return true
}

// unresolved returns the kept references that resolve in no registry, in walk
// order. A declared class has a registry only when the walk read every
// declaration; when it was cut short, references to one are claimed for none.
func (c *refsCollector) unresolved(decls declarations) []refSite {
	regs := c.keyed
	if !decls.truncated {
		regs = c.keyed.WithDeclarations(decls.ids)
	}
	var out []refSite
	for _, s := range c.sites {
		if reg, held := regs[s.idType]; held && !reg.Has(s.id) {
			out = append(out, s)
		}
	}
	return out
}

// label is the name a report spells the registry of class t with.
func (c *refsCollector) label(t reflect.Type) string {
	return c.all[t].Label
}
