// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"github.com/dexpace/morphic/ir"
)

// checkEmptyRefs asserts no reference is empty where its position requires one.
//
// checkReferentialIntegrity skips an empty ID because it is no reference, which
// leaves a position that requires one reported by nothing (GitHub #473). The
// answer cannot be keyed by type: Discriminator.Default is a TypeID whose empty
// value means "none". So it is per position, which ir.EmptyReferences reads
// from the one table pass.Validate reads too, so the two report an empty
// reference alike (GitHub #575).
func checkEmptyRefs(*ir.Document) walkCheck {
	found := &ir.EmptyReferences{}
	return walkCheck{
		visit: found.Visit,
		report: func(declarations) []Violation {
			empty := found.List()
			if len(empty) == 0 {
				return nil
			}
			vs := make([]Violation, 0, len(empty))
			for _, r := range empty {
				vs = append(vs, Violation{Code: r.Code(), Message: r.Message(), Path: r.Path})
			}
			return vs
		},
	}
}
