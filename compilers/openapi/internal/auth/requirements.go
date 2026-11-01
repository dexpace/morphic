// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"encoding/json/jsontext"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// Requirements lowers an OR-of-ANDs security list (docs/ir/auth.md) declared
// under base: "" at the root, else an operation's own declaration, never its
// mount. A nil list inherits the enclosing default, and an empty one stays []. A
// non-nil list yields one AuthRequirement per option, diagnosed at
// base+/security/<index>; an empty option {} means "no auth is one acceptable
// choice".
//
// A security written as no list arrives nil, beside the loader's report, and so
// does one under a local tag, with none (#1280). A list of entries that are no
// object yields no option, beside the loader's report, and collapses to nil, since
// [] would read as public.
func (r *Registry) Requirements(c lowering.Ctx, reqs []*soa.SecurityRequirement, base jsontext.Pointer) ([]ir.AuthRequirement, []ir.Diagnostic) {
	if reqs == nil {
		return nil, nil
	}
	out := make([]ir.AuthRequirement, 0, len(reqs))
	var diags []ir.Diagnostic
	for i, req := range reqs {
		if !writtenAsObject(req) {
			continue
		}
		pointer := base + ids.Ptr("security", strconv.Itoa(i))
		option, optionDiags := r.requirement(c, req, pointer)
		diags = append(diags, optionDiags...)
		out = append(out, option)
	}
	if len(reqs) > 0 && len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// requirement lowers one option declared at pointer. A requirement is a
// conjunction, so a member naming no mechanism stays, diagnosed: dropping it would
// demand less than the source does, and dropping the option would leave no
// requirement or one way fewer.
//
// Names and scopes come from the core: the model holds only a member whose
// scopes it could read (docs/parser-library.md#requirement-scopes), and one it
// left out still demands its scheme.
//
// A name is read as a securitySchemes key at every minor, a 3.2 URI included
// (#1212), and matched exactly, so a typo is reported and never guessed at.
func (r *Registry) requirement(c lowering.Ctx, req *soa.SecurityRequirement, pointer jsontext.Pointer,
) (ir.AuthRequirement, []ir.Diagnostic) {
	var uses []ir.SchemeUse
	var diags []ir.Diagnostic
	for name, member := range req.GetCore().All() {
		id := ids.Auth(name)
		if _, declared := r.byID[id]; !declared {
			id = r.unresolved(name)
			// "Unresolved", not "undeclared": the name may be declared as a $ref
			// resolving to nothing, or as an entry naming no mechanism (#294).
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedSecurityScheme, pointer,
				"security requirement references unresolved scheme %q: the requirement is kept, and the scheme has no mechanism", name))
		}
		scopes, _ := nodeview.Strings(member.ValueNode)
		uses = append(uses, ir.SchemeUse{Scheme: id, Scopes: scopes})
	}
	return ir.AuthRequirement{Schemes: uses}, diags
}

// writtenAsObject reports whether the document wrote req as an object, the only
// shape a requirement has.
//
// Null, a scalar and a sequence all unmarshal to a requirement with no members,
// like the {} meaning "no auth is one acceptable choice" (issue #284). The
// marshaller records a root node only for a value it could read as a mapping, so
// {} carries one inline or through an alias; being library behavior, that is
// tested through compiled documents. A lone `<<` naming no mapping still reads as
// {} (#1244).
//
// A nil req is unreachable from a parsed document; that guard is for a
// hand-built slice, as in unresolvableScheme.
func writtenAsObject(req *soa.SecurityRequirement) bool {
	return req != nil && req.GetRootNode() != nil
}
