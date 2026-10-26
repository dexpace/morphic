package pass

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// checkArgsOutsideGraphQL reports field arguments on models that are not
// reachable from a GraphQL binding (the only scope in which Property.Args is
// legal). With no GraphQL compiler yet, any Args is a violation.
func checkArgsOutsideGraphQL(doc *ir.Document) []ir.Diagnostic {
	reachable, truncated := graphqlReachableTypes(doc)
	if truncated {
		// The walk that built the reachability set saw a subset of the document,
		// so "not reached" no longer means "not reachable" — a GraphQL binding
		// past the cap goes unseen and the types it legitimises look illegal.
		// Fail open; checkGroupWalkTruncated reports why nothing is claimed here.
		return nil
	}
	var diags []ir.Diagnostic
	for _, id := range liveTypeIDs(doc) {
		m, ok := doc.Types[id].(*ir.Model)
		if !ok || reachable[id] {
			continue
		}
		for i, p := range m.Properties {
			if len(p.Args) == 0 {
				continue
			}
			diags = append(diags, diag(ir.SeverityError, "pass/args-outside-graphql",
				fmt.Sprintf("property %s/properties/%d declares field arguments outside a GraphQL binding", id, i),
				string(id)))
		}
	}
	return diags
}

// graphqlReachableTypes returns the set of type IDs transitively reachable from
// the operations that carry a GraphQL binding. The traversal is iterative with a
// visited set, so it terminates on cyclic type graphs. Operations are the roots
// and keep the reflection walk, since ir.TypeEdges has no operation form; each
// type after that is followed through ir.TypeEdges, which stops only where the
// reflection walk has, and checkDanglingRefs reports that.
//
// It reports whether the operation walk truncated, since a caller cannot tell an
// unreachable type from an unvisited one without it.
func graphqlReachableTypes(doc *ir.Document) (map[ir.TypeID]bool, bool) {
	seen := map[ir.TypeID]bool{}
	var queue []ir.TypeID
	truncated := ir.ForEachOperation(doc, func(op ir.Operation) {
		if op.Bindings.GraphQL != nil {
			queue = appendTypeIDs(queue, op)
		}
	})
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		queue = slices.AppendSeq(queue, ir.TypeEdges(doc.Types[id]))
	}
	return seen, truncated
}

// appendTypeIDs appends every TypeID reachable from an operation root to dst. It
// serves only graphqlReachableTypes's roots, since ir.TypeEdges covers type
// definitions and has no operation form. Truncation is dropped rather than
// reported: this feeds reachability, where a truncated walk can only
// under-report, and checkDanglingRefs already reports the same truncation over
// the whole document.
//
// Reachability follows one class, so it names that class rather than resolving
// against a document it is not handed, and it reads no path.
func appendTypeIDs(dst []ir.TypeID, root any) []ir.TypeID {
	ir.WalkValues(root, "", ir.VisitFunc(func(v reflect.Value, _ ir.WalkPath) bool {
		if v.Kind() == reflect.String && v.Type() == typeIDType && v.String() != "" {
			dst = append(dst, ir.TypeID(v.String()))
		}
		return true
	}))
	return dst
}

// typeIDType is the reflect.Type of ir.TypeID, the one reference class
// graphqlReachableTypes needs without a document to resolve against.
var typeIDType = reflect.TypeFor[ir.TypeID]()
