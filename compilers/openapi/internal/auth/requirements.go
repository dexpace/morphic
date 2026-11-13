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
// mount. A nil list stays nil, apart from an empty one, which stays []. A
// non-nil list yields one AuthRequirement per option, diagnosed at
// base+/security/<index>; an empty option {} means "no auth is one acceptable
// choice".
//
// A security written as no list arrives nil, beside the loader's report, and so
// does one under a local tag, with none (#1368). A list of entries naming no
// option (namesAnOption) yields none, beside the loader's report, and collapses
// to nil, since [] would read as public.
func (r *Registry) Requirements(c lowering.Ctx, reqs []*soa.SecurityRequirement, base jsontext.Pointer) ([]ir.AuthRequirement, []ir.Diagnostic) {
	if reqs == nil {
		return nil, nil
	}
	out := make([]ir.AuthRequirement, 0, len(reqs))
	var diags []ir.Diagnostic
	for i, req := range reqs {
		if !namesAnOption(req) {
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
// A key names a securitySchemes entry (named), matched exactly, so a typo is
// reported and never guessed at.
func (r *Registry) requirement(c lowering.Ctx, req *soa.SecurityRequirement, pointer jsontext.Pointer,
) (ir.AuthRequirement, []ir.Diagnostic) {
	var uses []ir.SchemeUse
	var diags []ir.Diagnostic
	for key, member := range req.GetCore().All() {
		name := r.named(c, key)
		id := ids.Auth(name)
		if _, declared := r.byID[id]; !declared {
			id = r.unresolved(name)
			// "Unresolved", not "undeclared": the name may be declared as a $ref
			// resolving to nothing, or as an entry naming no mechanism (#294).
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedSecurityScheme, pointer,
				"security requirement references unresolved scheme %q: the requirement is kept, and the scheme has no mechanism%s",
				key, unresolvedClauses(c)))
		}
		written, _ := nodeview.Strings(member.ValueNode)
		scopes, scopeDiags := requiredScopes(c, written, key, pointer)
		diags = append(diags, scopeDiags...)
		uses = append(uses, ir.SchemeUse{Scheme: id, Scopes: scopes})
	}
	return ir.AuthRequirement{Schemes: uses}, diags
}

// requiredScopes returns the scopes written, the list an option at pointer
// writes for the name it keys, each kept once, at its first occurrence, beside a
// warning at each repeat: the scopes a scheme is required to hold are a set.
// Scopes compare as the text written, and one scheme's list folds apart from
// another's.
func requiredScopes(c lowering.Ctx, written []string, name string, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	scopes, repeats := lowering.FirstOccurrences(written)
	var diags []ir.Diagnostic
	for _, r := range repeats {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.RepeatedEntry, pointer+ids.Ptr(name, strconv.Itoa(r.At)),
			"scope %q of scheme %q repeats the entry at index %d; it is kept once", written[r.At], name, r.First))
	}
	return scopes, diags
}

// namesAnOption reports whether req lowers to an option: an object naming a
// scheme, or {} as the document wrote it, meaning "no auth is one acceptable
// choice".
//
// Null, a scalar and a sequence unmarshal to a requirement with no members, like
// {} (issue #284); the marshaller records a root node only for a mapping, tested
// through compiled documents. A memberless mapping wrote {} only where every
// `<<` it merges through names a mapping: one naming none merges nothing
// (docs/parser-library.md#merge-keys), and reading the entry as {} would grant
// access the document never wrote. Only a hand-built requirement is nil or has a
// root that is no mapping.
func namesAnOption(req *soa.SecurityRequirement) bool {
	if req == nil {
		return false
	}
	root := req.GetRootNode()
	if root == nil || nodeview.NotMapping(root) {
		return false
	}
	return req.GetCore().Len() > 0 || !nodeview.MergesNothing(root)
}
