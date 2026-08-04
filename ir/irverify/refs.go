package irverify

import (
	"reflect"

	"github.com/dexpace/morphic/ir"
)

// refSite is one discovered ID reference: the class that made it a reference,
// its value, and where in the document it sits.
type refSite struct {
	idType reflect.Type
	id     string
	path   string
}

// collectRefs walks doc and returns every non-empty typed-ID reference regs
// recognizes, plus whether the bounded walk was truncated. It inspects both map
// keys and values: most keys are an entry's own ID and resolve trivially, but
// some — Service.Renames's map[TypeID]Naming keys — are genuine references into
// a registry that must resolve.
func collectRefs(doc *ir.Document, regs ir.Registries) ([]refSite, bool) {
	var sites []refSite
	truncated := ir.WalkValues(doc, "doc", func(v reflect.Value, path string) bool {
		if v.Kind() != reflect.String || v.String() == "" {
			return true
		}
		if _, isRef := regs[v.Type()]; isRef {
			sites = append(sites, refSite{idType: v.Type(), id: v.String(), path: path})
		}
		return true
	})
	return sites, truncated
}

// checkReferentialIntegrity asserts every discovered reference resolves in its
// registry, emitting one dangling-*-ref Violation per unresolved reference. It
// reports whether the bounded walk was truncated; Verify folds that into the
// document's one ir/walk-truncated violation.
//
// What counts as a reference comes from Document's own shape rather than a table
// written here: a registry added to Document is checked the moment it exists,
// under a code spelled from the ID type it is keyed by — the same derivation
// pass.Validate reports the identical defect under, so one defect reads as one
// code whichever checker a caller runs.
//
// Two ID classes are outside what a registry-driven walk can resolve, and both
// stay out rather than growing a second implementation here. ir.ServiceID names
// a position in Document.Services; ir.PropID names a position inside its model,
// and resolving one means collecting the ir.Property values a document declares
// and looking the ID up among them, which pass.Validate's checkPropIDRefs does.
func checkReferentialIntegrity(doc *ir.Document) ([]Violation, bool) {
	regs := ir.DocumentRegistries(doc)
	sites, truncated := collectRefs(doc, regs)
	var vs []Violation
	for _, s := range sites {
		reg := regs[s.idType]
		if reg.Has(s.id) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/dangling-" + ir.RefNoun(s.idType) + "-ref",
			Message: "reference " + s.id + " does not resolve in " + reg.Label,
			Path:    s.path,
		})
	}
	return vs, truncated
}
