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

// collectRefs walks doc and returns every non-empty typed-ID reference regs
// recognizes, plus whether the bounded walk was truncated. It inspects both map
// keys and values: most keys are an entry's own ID and resolve trivially, but
// some — Service.Renames's map[TypeID]Naming keys — are genuine references into
// a registry that must resolve.
//
// An empty ID is skipped because it is no reference: some positions spell
// "none" with one (Discriminator.Default). Whether a position may hold one is a
// different question: answered for ir.TypeRef by checkTypeRefs and for the bare
// ID positions by checkEmptyRefs (GitHub #473).
func collectRefs(doc *ir.Document, regs ir.Registries) ([]refSite, bool) {
	var sites []refSite
	truncated := ir.WalkValues(doc, ir.DocumentPath, func(v reflect.Value, path string) bool {
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
// registry, reporting each unresolved one as dangling.
//
// The registries come from Document's own shape, so a new one is checked at
// once, under the code pass.Validate gives it. ir.Operation and ir.Service have
// no map, so ir.Registries.WithDeclarations supplies them from the declared
// identities (GitHub #50), unless the declaration walk truncated: a partial
// walk would report a reference to an unreached operation as dangling.
//
// ir.PropID stays out: pass.Validate's checkPropIDRefs resolves it against its
// model. The returned flag, which Verify reports as ir/walk-truncated, folds in
// decls.truncated rather than relying on collectRefs tripping the cap first
// (GitHub #55).
func checkReferentialIntegrity(doc *ir.Document, decls declarations) ([]Violation, bool) {
	regs := ir.DocumentRegistries(doc)
	if !decls.truncated {
		regs = regs.WithDeclarations(decls.ids)
	}
	sites, truncated := collectRefs(doc, regs)
	truncated = truncated || decls.truncated
	var vs []Violation
	for _, s := range sites {
		reg := regs[s.idType]
		if reg.Has(s.id) {
			continue
		}
		vs = append(vs, Violation{
			Code:    "ir/dangling-" + ir.RefNoun(s.idType) + "-ref",
			Message: "reference " + strconv.Quote(s.id) + " does not resolve in " + reg.Label,
			Path:    s.path,
		})
	}
	return vs, truncated
}
