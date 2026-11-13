// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lowering

import (
	"slices"

	"github.com/dexpace/morphic/ir"
)

// ServerName reads from unmodeled, a server's, the name the policy maps
// (TargetServerName), and marks prov when it returns one. The caller asks only
// of a server its document names in no other way: a name the document declares
// is its own, and no key's.
func (p Promoter) ServerName(unmodeled ir.Unmodeled, prov *ir.Provenance) (string, []ir.Diagnostic) {
	if prov == nil {
		return "", nil
	}
	name, found, diags := firstStated(unmodeled, p.targets[TargetServerName], textReading(TargetServerName))
	if found {
		prov.Inferred = ir.JoinInferred(prov.Inferred, ExtensionPromotionHeuristic)
	}
	return name, diags
}

// AllowReserved reports whether the extensions kept in unmodeled, a
// parameter's, ask for its value's reserved characters to be sent unescaped
// (TargetAllowReserved), and marks prov when they do. The caller asks only of a
// parameter writing no `allowReserved`, which decides for itself.
func (p Promoter) AllowReserved(unmodeled ir.Unmodeled, prov *ir.Provenance) (bool, []ir.Diagnostic) {
	if prov == nil {
		return false, nil
	}
	reserved, _, diags := firstStated(unmodeled, p.targets[TargetAllowReserved], reading[bool]{
		target: TargetAllowReserved,
		want:   "is not a boolean",
		read: func(src ExtensionSource, raw ir.RawValue) (bool, statement) {
			if len(src.Values) > 0 {
				return listed(p.targets, TargetAllowReserved, src, raw)
			}
			return statesBoolean(raw)
		},
	})
	if reserved {
		prov.Inferred = ir.JoinInferred(prov.Inferred, ExtensionPromotionHeuristic)
	}
	return reserved, diags
}

// TokenEndpointAuthMethods reads from unmodeled, an OAuth flow's, the ways a
// client authenticates to its token endpoint (TargetTokenEndpointAuthMethods),
// and marks inferred when it returns any. A string is one method and a list of
// strings several; a list holding empty text or a method twice is a misfit, as
// the field is a set of names, and so is any other value. Nothing checks a
// method against the registry, which is open.
func (p Promoter) TokenEndpointAuthMethods(unmodeled ir.Unmodeled, inferred *ir.Inferred) ([]string, []ir.Diagnostic) {
	if inferred == nil {
		return nil, nil
	}
	text, found, diags := firstStated(unmodeled, p.targets[TargetTokenEndpointAuthMethods], reading[string]{
		target: TargetTokenEndpointAuthMethods,
		want:   "is neither a method nor a list of distinct methods",
		read: func(_ ExtensionSource, raw ir.RawValue) (string, statement) {
			methods, ok := listedText(raw)
			switch {
			case !ok || len(slices.Compact(slices.Sorted(slices.Values(methods)))) != len(methods):
				return "", misfit
			case len(methods) == 0:
				return "", statesNothing
			default:
				return string(asList(raw)), states
			}
		},
	})
	if !found {
		return nil, diags
	}
	methods, _ := listedText(ir.RawValue(text))
	*inferred = ir.JoinInferred(*inferred, ExtensionPromotionHeuristic)
	return methods, diags
}
