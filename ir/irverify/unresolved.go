// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify

import (
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// checkUnresolvedSchemes holds the schemes of kind ir.AuthKindUnresolved to
// what the kind tells a reader (docs/ir/auth.md).
//
// The kind and ir.IDSpaceUnresolved go together both ways: outside the space a
// pointer could reach the scheme, which is invariant 3's corollary, and inside
// it a mechanism sits where an emitter reads none. A scheme with no space to
// compare is checkIDs' to report. A field saying how to authenticate would
// contradict the kind, while a name and text about the scheme do not.
func checkUnresolvedSchemes(doc *ir.Document) []Violation {
	var vs []Violation
	for id, scheme := range doc.Auth {
		unresolved := scheme.Kind == ir.AuthKindUnresolved
		space, wellFormed := ir.IDSpace(ir.IDKindAuth, string(id))
		if wellFormed && unresolved != (space == ir.IDSpaceUnresolved) {
			vs = append(vs, Violation{
				Code: "ir/unresolved-auth-space",
				Message: "auth scheme of kind " + strconv.Quote(string(scheme.Kind)) + " is addressed in space " +
					strconv.Quote(space) + ", and the kind unresolved and the space unresolved go together",
				Path: "auth[" + string(id) + "]",
			})
		}
		if unresolved && statesMechanism(scheme) {
			vs = append(vs, Violation{
				Code:    "ir/unresolved-auth-has-mechanism",
				Message: "auth scheme of kind unresolved states how to authenticate",
				Path:    "auth[" + string(id) + "]",
			})
		}
	}
	return vs
}

// statesMechanism reports whether scheme fills a field saying how a client
// authenticates. TestVerify_AnUnresolvedSchemeStatesNoMechanism holds the list
// to ir.AuthScheme, field by field.
func statesMechanism(scheme ir.AuthScheme) bool {
	return scheme.In != "" || scheme.KeyName != "" || scheme.Scheme != "" || scheme.BearerFormat != "" ||
		len(scheme.Flows) > 0 || scheme.OAuth2MetadataURL != "" || scheme.OpenIDConnectURL != "" ||
		scheme.Credentials != nil
}
