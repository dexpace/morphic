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
// (ir.IDReferences, which ir/irverify resolves through too), not a list of field
// names, which would drift behind the IR unnoticed. Integer-index references are
// enumerated by hand in validate_indices.go, and ir.PropID is checkPropIDRefs's.
// Provenance.Source is irverify's: a stale index is a compiler bug, and this
// pass's own ir.NoSource diagnostics would make a document-wide check report
// its own output.
func checkDanglingRefs(refs *ir.IDReferences, truncated bool) []ir.Diagnostic {
	var diags []ir.Diagnostic
	if truncated {
		diags = append(diags, diag(ir.SeverityError, "ir/walk-truncated",
			"document nests deeper than the bounded reference walk; some references went unchecked",
			ir.DocumentPath))
	}
	for _, ref := range byLocation(refs.Unresolved(truncated)) {
		noun := ir.RefNoun(ref.Class)
		diags = append(diags, diag(ir.SeverityError, "ir/dangling-"+noun+"-ref",
			fmt.Sprintf("%s reference %q at %s resolves to no %s in the registry", noun, ref.ID, ref.Path, noun),
			ref.Path))
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
func checkPropIDRefs(props *propIDSites, refs *ir.IDReferences) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, ref := range byLocation(props.sites) {
		if refs.Declares(propIDType, ref.ID) {
			continue
		}
		diags = append(diags, diag(ir.SeverityError, "ir/dangling-prop-ref",
			fmt.Sprintf("prop reference %q at %s resolves to no property declared in the document", ref.ID, ref.Path),
			ref.Path))
	}
	return diags
}

// byLocation sorts sites by location, in place, and returns them.
//
// Distinct sites have distinct paths in every shape the IR declares, but two
// embedded structs contributing a same-named promoted field would collide; a
// stable sort keeps the deterministic walk order in that case rather than
// leaving the pair to sort.Interface's unspecified swap order. Determinism
// (invariant 7) rests on the ordered map walk: sorting cannot repair a site set
// whose membership varies under a randomized walk of an aliased graph.
func byLocation(sites []ir.IDReference) []ir.IDReference {
	slices.SortStableFunc(sites, func(a, b ir.IDReference) int {
		return strings.Compare(a.Path, b.Path)
	})
	return sites
}

// propIDType is the reflect.Type of ir.PropID. A PropID references no
// document-level registry — a property is a position inside its model — so both
// halves of resolving one come from the walk itself: the sites that carry a
// PropID, and the nodes that declare them.
var propIDType = reflect.TypeFor[ir.PropID]()

// propIDSites gathers, as an ir.Visitor, the ir.PropID references a walk
// reaches that may not resolve. One naming a property refs has already read,
// the property's own ID among them, resolves, so it is not kept and its path
// never rendered.
//
// A PropID reached as a map key is left out. The one PropID-keyed map the IR
// declares is Content.Encoding, and checkEncodingKeys resolves those keys
// against the properties of the model the content names, a tighter claim about
// the same defect. Reporting both would give one defect two codes, which the
// package doc rules out.
type propIDSites struct {
	refs  *ir.IDReferences
	sites []ir.IDReference
}

// Visit keeps v if it is a PropID reference.
func (p *propIDSites) Visit(v reflect.Value, at ir.WalkPath) bool {
	if v.Kind() != reflect.String || v.Type() != propIDType || v.String() == "" {
		return true
	}
	if p.refs.Declares(propIDType, v.String()) {
		return true
	}
	where := at.String()
	if !strings.HasSuffix(where, ir.MapKeySuffix) {
		p.sites = append(p.sites, ir.IDReference{Class: propIDType, ID: v.String(), Path: where})
	}
	return true
}
