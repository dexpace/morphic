package pass

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// checkDanglingRefs reports every typed-ID reference one walk kept that
// resolves to no entry in the registry declaring its class — a TypeRef.Target
// into doc.Types, a SchemeUse.Scheme into doc.Auth.
//
// Sites and registries come from reflection over the document's own shape
// (ir.WalkValues, ir.DocumentRegistries), not a list of field names, which
// would drift behind the IR unnoticed. Integer-index references are enumerated
// by hand in validate_indices.go, and ir.PropID is checkPropIDRefs's.
// Provenance.Source is irverify's: a stale index is a compiler bug, and this
// pass's own ir.NoSource diagnostics would make a document-wide check report
// its own output.
func checkDanglingRefs(refs *refSites, decls []ir.IDDeclaration, truncated bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if truncated {
		diags = append(diags, diag(ir.SeverityError, "ir/walk-truncated",
			"document nests deeper than the bounded reference walk; some references went unchecked",
			ir.DocumentPath))
	}
	for _, s := range refs.unresolved(decls, truncated) {
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
func checkPropIDRefs(props *propIDSites, decls []ir.IDDeclaration) []ir.Diagnostic {
	declared := map[ir.PropID]bool{}
	for _, d := range decls {
		if d.Class == propIDType {
			declared[ir.PropID(d.ID)] = true
		}
	}
	var diags []ir.Diagnostic
	for _, s := range byLocation(props.sites) {
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

// refSites gathers, as an ir.Visitor, the references a walk reaches that may
// not resolve. The traversal is ir.WalkValues, shared with ir/irverify so that
// one document has one walk: the same bound, cycle guard, order and path
// spelling.
//
// A reference into a registry Document keys by its class resolves or not as the
// walk reaches it, so only one that does not is kept, path and all. A declared
// class's registry is complete only once the walk is over, so every reference to
// one is kept for unresolved to settle. An empty ID is no reference.
type refSites struct {
	keyed ir.Registries
	all   ir.Registries
	sites []refSite
}

// newRefSites returns a gatherer of doc's references.
func newRefSites(doc *ir.Document) *refSites {
	keyed := ir.DocumentRegistries(doc)
	return &refSites{keyed: keyed, all: keyed.WithDeclarations(nil)}
}

// Visit keeps v if it is a reference that may not resolve.
func (r *refSites) Visit(v reflect.Value, at ir.WalkPath) bool {
	if v.Kind() != reflect.String || v.String() == "" {
		return true
	}
	t := v.Type()
	if _, isRef := r.all[t]; !isRef {
		return true
	}
	if reg, keyed := r.keyed[t]; keyed && reg.Has(v.String()) {
		return true
	}
	r.sites = append(r.sites, refSite{idType: t, id: v.String(), where: at.String()})
	return true
}

// unresolved returns the kept references that resolve in no registry, sorted by
// location. Document has no map for OpID, ServiceID or GroupID: an ir.Operation
// and an ir.OperationGroup are declared in the Service→OperationGroup tree and
// an ir.Service in a slice (GitHub #50), so their registries come from decls.
//
// A registry built from a truncated walk says "not declared" of a node it never
// reached, so a legitimate operation past the depth bound would be reported as
// dangling. Dropping those classes can only under-report; checkDanglingRefs's
// walk-truncated diagnostic says why nothing is claimed for them.
func (r *refSites) unresolved(decls []ir.IDDeclaration, truncated bool) []refSite {
	regs := r.keyed
	if !truncated {
		regs = r.keyed.WithDeclarations(decls)
	}
	var out []refSite
	for _, s := range r.sites {
		if reg, held := regs[s.idType]; held && !reg.Has(s.id) {
			out = append(out, s)
		}
	}
	return byLocation(out)
}

// byLocation sorts sites by location, in place, and returns them.
//
// Distinct sites have distinct paths in every shape the IR declares, but two
// embedded structs contributing a same-named promoted field would collide; a
// stable sort keeps the deterministic walk order in that case rather than
// leaving the pair to sort.Interface's unspecified swap order. Determinism
// (invariant 7) rests on the ordered map walk: sorting cannot repair a site set
// whose membership varies under a randomized walk of an aliased graph.
func byLocation(sites []refSite) []refSite {
	slices.SortStableFunc(sites, func(a, b refSite) int {
		return strings.Compare(a.where, b.where)
	})
	return sites
}

// propIDType is the reflect.Type of ir.PropID. A PropID references no
// document-level registry — a property is a position inside its model — so both
// halves of resolving one come from the walk itself: the sites that carry a
// PropID, and the nodes that declare them.
var propIDType = reflect.TypeFor[ir.PropID]()

// propIDSites gathers, as an ir.Visitor, every ir.PropID reference a walk
// reaches. The declarations it is resolved against come from the same walk, so
// a new PropID-carrying field or Property-bearing list is covered at once.
//
// A PropID reached as a map key is left out. The one PropID-keyed map the IR
// declares is Content.Encoding, and checkEncodingKeys resolves those keys
// against the properties of the model the content names, a tighter claim about
// the same defect. Reporting both would give one defect two codes, which the
// package doc rules out.
type propIDSites struct {
	sites []refSite
}

// Visit keeps v if it is a PropID reference.
func (p *propIDSites) Visit(v reflect.Value, at ir.WalkPath) bool {
	if v.Kind() != reflect.String || v.Type() != propIDType || v.String() == "" {
		return true
	}
	where := at.String()
	if !strings.HasSuffix(where, ir.MapKeySuffix) {
		p.sites = append(p.sites, refSite{idType: propIDType, id: v.String(), where: where})
	}
	return true
}
