// Package reach holds what cmd/morphic-vet refuses, which scripts/vet-rules.sh
// requires go vet to report, and nothing else: that is how it shows the run
// reads the packages it is given.
package reach

import "github.com/dexpace/morphic/ir"

// Registry builds a registry, which cmd/morphic-vet refuses outside ir and
// compilers/compile.
func Registry() ir.TypeRegistry { return ir.TypeRegistry{} }
