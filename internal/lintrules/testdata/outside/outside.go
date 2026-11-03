// Package outside sits beside the tree the tests lay out like the module, so
// no rule holds it.
package outside

import (
	"net/url"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// Outside builds registries and a slice of the service tree, reads a fragment
// and reads a document's version, which draws nothing here.
func Outside(doc *soa.OpenAPI) (ir.TypeRegistry, []ir.Operation, map[ir.AuthID]ir.AuthScheme, error) {
	_, err := url.QueryUnescape("a+b")
	if doc.GetOpenAPI() == "" {
		return nil, nil, nil, err
	}
	return ir.TypeRegistry{}, []ir.Operation{}, map[ir.AuthID]ir.AuthScheme{}, err
}
