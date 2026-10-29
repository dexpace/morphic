package irverify

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkOAuthScopes asserts no OAuth flow offers one scope name twice.
//
// A flow's scopes were a map, which made each name unique by construction. As
// a list in source order (ir-design §9) nothing else holds them to it, and two
// entries for one scope leave a consumer two descriptions to choose between. A
// source that writes a name twice is the compiler's to fold, so a repeat here
// is the compiler's. It is reported at the later entry.
func checkOAuthScopes(doc *ir.Document) []Violation {
	var vs []Violation
	// Every flow is read through this one, so a flow costs no map of its own
	// however many scopes it offers.
	var offered reusedMap[string, bool]
	for id, scheme := range doc.Auth {
		for i, flow := range scheme.Flows {
			seen := offered.emptied()
			for j, scope := range flow.Scopes {
				if seen[scope.Name] {
					vs = append(vs, Violation{
						Code:    "ir/oauth-scope-duplicate",
						Message: "oauth flow offers scope " + strconv.Quote(scope.Name) + " more than once",
						Path:    "auth[" + string(id) + "].Flows[" + strconv.Itoa(i) + "].Scopes[" + strconv.Itoa(j) + "]",
					})
				}
				seen[scope.Name] = true
			}
		}
	}
	return vs
}
