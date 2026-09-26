package operation

import (
	"cmp"
	"slices"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxAliasHops bounds the alias chain declaringNode follows. The pre-parse
// refusals have rejected a recursive anchor before anything is lowered, so no
// document reaches it.
const maxAliasHops = 100

// operationIDClaims collects the operationId each lowered operation claims, so
// that uniqueness is judged once the whole service is lowered rather than as
// each claim arrives. Judging on arrival reported whichever claim was lowered
// second, so where the finding landed depended on declaration order.
//
// It owns the rule. The parser's validator checks it too, but it counts an
// operation that a YAML alias mounts twice as two operations, never sees one a
// $ref mounts twice, and never sees a webhook or callback operation at all. The
// two reuse mechanisms therefore disagreed, and a repeat the document does write
// went unreported wherever that validator does not look (GitHub #502). The
// loader drops the validator's finding.
type operationIDClaims struct {
	names  []string // each operationId, in the order it was first claimed
	claims map[string][]operationIDClaim
}

// operationIDClaim is one operation claiming an operationId: the node declaring
// it, which is what tells one declaration mounted twice from two declarations
// writing the same id, and the pointers the operation was lowered with.
type operationIDClaim struct {
	node *yaml.Node
	ptrs opPointers
}

func newOperationIDClaims() *operationIDClaims {
	return &operationIDClaims{claims: map[string][]operationIDClaim{}}
}

// add records the claim src makes at ptrs. An operation with no operationId
// claims nothing: emitters synthesize its name from the method and path.
func (o *operationIDClaims) add(src *soa.Operation, ptrs opPointers) {
	name := src.GetOperationID()
	if name == "" {
		return
	}
	if _, seen := o.claims[name]; !seen {
		o.names = append(o.names, name)
	}
	o.claims[name] = append(o.claims[name], operationIDClaim{node: declaringNode(src.GetRootNode()), ptrs: ptrs})
}

// report judges every operationId claimed more than once, in first-claim order.
func (o *operationIDClaims) report(c lowering.Ctx) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, name := range o.names {
		if claims := o.claims[name]; len(claims) > 1 {
			diags = append(diags, judge(c, name, declarations(claims))...)
		}
	}
	return diags
}

// judge reports one operationId's claims. Each element of decls is one
// declaration's mounts, ordered as declarations orders them, so every finding
// names the same first claim whatever order the document declares them in.
//
// A declaration mounted more than once is a warning at each later mount: a path
// item or an operation that a $ref, a YAML alias or a merge key reuses. The
// document writes the id once, but each mount is an operation of its own, and an
// emitter renders them all under one identifier.
//
// A second declaration writing the same id is an error at that declaration. The
// document itself repeats the id, which OpenAPI forbids among all the operations
// it describes, webhooks and callbacks included.
func judge(c lowering.Ctx, name string, decls [][]opPointers) []ir.Diagnostic {
	var diags []ir.Diagnostic
	first := decls[0][0]
	for i, mounts := range decls {
		if i > 0 {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.ConflictingOperationID, mounts[0].decl,
				"operationId %q is also used by the operation declared at %s; "+
					"OpenAPI requires it to be unique across the API", name, first.decl))
		}
		for _, again := range mounts[1:] {
			diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DuplicateOperationID, again.mount,
				"operationId %q is also used by the operation at %s: both mount one declaration, "+
					"and OpenAPI requires the id to be unique across the API", name, mounts[0].mount))
		}
	}
	return diags
}

// declarations groups claims by the node that declares them, and orders both the
// mounts within a declaration and the declarations themselves by mount pointer,
// a property of the document rather than of the order it was lowered in.
//
// A claim with no node is taken as a declaration of its own. That is the
// stricter reading: it can turn a reuse into a conflict, but never a conflict
// into a reuse.
func declarations(claims []operationIDClaim) [][]opPointers {
	decls := make([][]opPointers, 0, len(claims))
	index := make(map[*yaml.Node]int, len(claims))
	for _, cl := range claims {
		i, seen := index[cl.node]
		if !seen {
			i = len(decls)
			decls = append(decls, nil)
			if cl.node != nil {
				index[cl.node] = i
			}
		}
		decls[i] = append(decls[i], cl.ptrs)
	}
	byMount := func(a, b opPointers) int { return cmp.Compare(a.mount, b.mount) }
	for _, mounts := range decls {
		slices.SortFunc(mounts, byMount)
	}
	slices.SortFunc(decls, func(a, b []opPointers) int { return byMount(a[0], b[0]) })
	return decls
}

// declaringNode is the node that declares an operation. One reached through a
// YAML alias is built from the alias node itself, so the alias is followed to the
// node it names: that is what makes an alias's reuse read as the $ref form's
// does, one declaration mounted twice.
func declaringNode(n *yaml.Node) *yaml.Node {
	for hops := 0; n != nil && n.Kind == yaml.AliasNode && n.Alias != nil && hops < maxAliasHops; hops++ {
		n = n.Alias
	}
	return n
}
