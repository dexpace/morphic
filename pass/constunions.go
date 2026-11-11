// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

var _ Pass = ConstUnions{}

// ConstUnionsName is the stable name of the [ConstUnions] pass.
const ConstUnionsName = "const-unions"

// ConstUnions rewrites a closed union of strings as the closed [ir.Enum] of
// them, at the union's own ID, so a consumer meets one closed set of strings in
// one shape however the document spelled it
// (docs/compilers/openapi.md#string-const-union). A variant holding one value,
// a string Literal or a one-member Enum, gives its member what it says of the
// value; one holding several gives its members. Where the Enum could not say
// all the union does, the union stays. It holds no state, so it is safe for
// concurrent use.
type ConstUnions struct{}

// Name returns [ConstUnionsName].
func (ConstUnions) Name() string { return ConstUnionsName }

// Run returns doc with each union constEnum reads as an Enum replaced by it,
// and each type only those unions named, which nothing references once they
// are gone, removed. It returns doc unchanged where no union is one, and with
// nothing once ctx is done.
func (ConstUnions) Run(ctx context.Context, doc *ir.Document) (*ir.Document, []ir.Diagnostic) {
	if doc == nil {
		return doc, nil
	}
	enums, branches := constEnums(doc)
	if len(enums) == 0 || ctx.Err() != nil {
		return doc, nil
	}
	e := ir.NewEditor(doc)
	for _, enum := range enums {
		e.ReplaceType(enum)
	}
	out, diags := e.Finish()

	dead, err := unreferenced(ctx, out, branches)
	if err != nil {
		return doc, nil
	}
	if len(dead) == 0 {
		return out, diags
	}
	trim := ir.NewEditor(out)
	for _, id := range dead {
		trim.RemoveType(id)
	}
	trimmed, more := trim.Finish()
	return trimmed, append(diags, more...)
}

// constEnums returns, in ID order, the Enum of each union of doc that
// constEnum reads as one, and the anonymous types their variants name, which
// the Enums hold the values of. A union off its own key is left to validate.
func constEnums(doc *ir.Document) ([]*ir.Enum, []ir.TypeID) {
	var enums []*ir.Enum
	named := map[ir.TypeID]bool{}
	spaces := doc.IDSpaces[ir.IDKindEnumMember]
	for id, td := range doc.Types {
		u, isUnion := td.(*ir.Union)
		if !isUnion || u == nil || u.ID != id {
			continue
		}
		enum, ok := constEnum(doc.Types, spaces, u)
		if !ok {
			continue
		}
		enums = append(enums, enum)
		for _, v := range u.Variants {
			named[v.Type.Target] = true
		}
	}
	slices.SortFunc(enums, func(a, b *ir.Enum) int { return strings.Compare(string(a.ID), string(b.ID)) })
	return enums, slices.Sorted(maps.Keys(named))
}
