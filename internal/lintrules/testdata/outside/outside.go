// Package outside sits beside the tree the tests lay out like the module, so
// no rule holds it.
package outside

import (
	"net/url"

	"github.com/dexpace/morphic/ir"
)

// Outside builds a registry and reads a fragment, which draws nothing here.
func Outside() (ir.TypeRegistry, error) {
	_, err := url.QueryUnescape("a+b")
	return ir.TypeRegistry{}, err
}
