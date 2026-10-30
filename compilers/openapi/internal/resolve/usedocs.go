// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package resolve

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// siblingDocs is what a speakeasy Reference wrapper exposes of the summary and
// description written beside its `$ref`. Populate reads the two only for an
// entry that holds a reference, so an inline entry reports both empty.
type siblingDocs interface {
	GetSummary() string
	GetDescription() string
}

// UseDocs folds the summary and description written beside each `$ref` of ref's
// chain over docs, those of the declaration the chain ends at. It folds from the
// declaration outward, field by field, so the use site's own siblings win, an
// alias's beat what it names, and a field a hop omits leaves the one beneath it
// standing (GitHub #610). OpenAPI gives such a sibling "no effect" where the
// object type allows none, but the IR has a docs field at each position this
// serves, so it is kept rather than dropped
// (docs/compilers/openapi.md#reference-object-siblings).
func UseDocs[T, S any, R interface {
	*S
	Referenced[T, S]
	siblingDocs
}](ref R, docs ir.Docs) ir.Docs {
	hops := make([]R, 0, 1)
	for range maxRefChain {
		if ref == nil {
			break
		}
		hops = append(hops, ref)
		info := ref.GetReferenceResolutionInfo()
		if info == nil {
			break
		}
		ref = R(info.Object)
	}
	for _, hop := range slices.Backward(hops) {
		if summary := hop.GetSummary(); summary != "" {
			docs.Summary = summary
		}
		if description := hop.GetDescription(); description != "" {
			docs.Description = description
		}
	}
	return docs
}
