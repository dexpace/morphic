package pass

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkDanglingRefs reports every typed-ID reference that resolves to no entry
// in the registry declaring that class of ID — a TypeRef.Target into doc.Types,
// a SchemeUse.Scheme into doc.Auth — wherever it sits. decls and declTruncated
// are ir.DeclaredIDs(doc).
//
// Sites and registries come from reflection over the document's own shape
// (ir.WalkValues, ir.DocumentRegistries), not a list of field names, which
// would drift behind the IR unnoticed. Integer-index references are enumerated
// by hand in validate_indices.go, and ir.PropID is checkPropIDRefs's.
// Provenance.Source is irverify's: a stale index is a compiler bug, and this
// pass's own ir.NoSource diagnostics would make a document-wide check report
// its own output.
func checkDanglingRefs(doc *ir.Document, decls []ir.IDDeclaration, declTruncated bool) []ir.Diagnostic {
	// Document has no map for OpID, ServiceID or GroupID: an ir.Operation and an
	// ir.OperationGroup are declared in the Service→OperationGroup tree and an
	// ir.Service in a slice (GitHub #50), so their registries come from the
	// identities the nodes declare.
	regs := ir.DocumentRegistries(doc)
	// A registry built from a truncated walk says "not declared" of a node it
	// never reached, so a legitimate operation past the depth bound would be
	// reported as dangling. Dropping both classes can only under-report; the
	// walk-truncated diagnostic below says why nothing is claimed for them.
	if !declTruncated {
		regs = regs.WithDeclarations(decls)
	}
	sites, truncated := collectRefs(doc, ir.DocumentPath, func(t reflect.Type) bool {
		_, isRegistry := regs[t]
		return isRegistry
	})
	var diags []ir.Diagnostic
	if truncated || declTruncated {
		diags = append(diags, diag(ir.SeverityError, "ir/walk-truncated",
			"document nests deeper than the bounded reference walk; some references went unchecked",
			ir.DocumentPath))
	}
	for _, s := range sites {
		// A site whose class this document declares no registry for cannot
		// resolve; the zero ir.Registry reports so rather than panicking, which
		// keeps the pass report-only if a caller ever mixes in sites collected
		// against another document.
		if regs[s.idType].Has(s.id) {
			continue
		}
		noun := ir.RefNoun(s.idType)
		diags = append(diags, diag(ir.SeverityError, "ir/dangling-"+noun+"-ref",
			fmt.Sprintf("%s reference %q at %s resolves to no %s in the registry", noun, s.id, s.where, noun),
			s.where))
	}
	return diags
}

// checkPropIDRefs reports every PropID a document carries that names no property
// it declares.
//
// A property is a position inside its model and addresses no registry, so
// checkDanglingRefs cannot see a PropID. The claim is membership: the ID names
// a property declared somewhere. Reflection supplies the sites but not each
// path's root, which only a hand-kept list of fields could add;
// checkDiscriminators makes the model-scoped claim for Discriminator.Property,
// whose root is the model declaring it.
//
// Docs.Description's {t:TypeID} tokens are left unchecked: reaching them needs
// a token parser, and a false positive in prose is noisier than a missing
// check.
func checkPropIDRefs(doc *ir.Document) []ir.Diagnostic {
	sites, declared := collectPropIDs(doc, ir.DocumentPath)
	var diags []ir.Diagnostic
	for _, s := range sites {
		if declared[ir.PropID(s.id)] {
			continue
		}
		diags = append(diags, diag(ir.SeverityError, "ir/dangling-prop-ref",
			fmt.Sprintf("prop reference %q at %s resolves to no property declared in the document", s.id, s.where),
			s.where))
	}
	return diags
}

// refSite is one discovered ID reference: the class that made it a reference,
// its value, and a human-readable location used for diagnostic provenance.
type refSite struct {
	idType reflect.Type
	id     string
	where  string
}

// collectRefs returns every reference reachable from root whose Go type isRef
// accepts, sorted by location, and reports whether the depth cap truncated the
// walk.
//
// The traversal is ir.WalkValues, shared with ir/irverify so that one document
// has one walk: the same bound, cycle guard, order and path spelling. The
// integer-index checks in validate_indices.go stay rooted at a stable ID rather
// than doc, since a spec author reads those and an ID outlives a position.
//
// Determinism (invariant 7) rests on the ordered map walk: sorting cannot
// repair a site set whose membership varies under a randomized walk of an
// aliased graph.
func collectRefs(root any, path string, isRef func(reflect.Type) bool) ([]refSite, bool) {
	return collectWalk(root, path, isRef, nil)
}

// collectWalk is collectRefs with an optional per-struct hook, so a caller can
// collect declarations alongside references in one traversal rather than
// standing up a second bounded walk beside this one.
func collectWalk(root any, path string, isRef func(reflect.Type) bool, onStruct func(reflect.Value)) ([]refSite, bool) {
	var sites []refSite
	truncated := ir.WalkValues(root, path, func(v reflect.Value, at string) bool {
		if v.Kind() == reflect.String && v.String() != "" && isRef(v.Type()) {
			sites = append(sites, refSite{idType: v.Type(), id: v.String(), where: at})
		}
		if onStruct != nil && v.Kind() == reflect.Struct {
			onStruct(v)
		}
		return true
	})
	// Distinct sites have distinct paths in every shape the IR declares, but two
	// embedded structs contributing a same-named promoted field would collide;
	// a stable sort keeps the deterministic walk order in that case rather than
	// leaving the pair to sort.Interface's unspecified swap order.
	slices.SortStableFunc(sites, func(a, b refSite) int {
		return strings.Compare(a.where, b.where)
	})
	return sites, truncated
}

// propIDType is the reflect.Type of ir.PropID. A PropID references no
// document-level registry — a property is a position inside its model — so both
// halves of resolving one come from the walk itself: the sites that carry a
// PropID, and the nodes that declare them.
var propIDType = reflect.TypeFor[ir.PropID]()

// collectPropIDs returns every ir.PropID reference reachable from root together
// with the set of PropIDs the same traversal saw declared, on an ir.Property.
// Both halves come from the value graph, so a new PropID-carrying field or
// Property-bearing list is covered at once.
//
// A PropID reached as a map key is left out. The one PropID-keyed map the IR
// declares is Content.Encoding, and checkEncodingKeys resolves those keys
// against the properties of the model the content names, a tighter claim about
// the same defect. Reporting both would give one defect two codes, which the
// package doc rules out.
func collectPropIDs(root any, path string) ([]refSite, map[ir.PropID]bool) {
	declared := map[ir.PropID]bool{}
	sites, _ := collectWalk(root, path,
		func(t reflect.Type) bool { return t == propIDType },
		func(v reflect.Value) {
			if class, id, declares := ir.DeclaredIDOf(v); declares && class == propIDType && id != "" {
				declared[ir.PropID(id)] = true
			}
		})
	// Truncation is dropped rather than returned: this walks the same document
	// to the same cap as checkDanglingRefs, which reports ir/walk-truncated for
	// both.
	return slices.DeleteFunc(sites, func(s refSite) bool {
		return strings.HasSuffix(s.where, ir.MapKeySuffix)
	}), declared
}
