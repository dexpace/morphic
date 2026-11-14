// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth

import (
	"encoding/json/jsontext"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
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
// A key names a securitySchemes entry of the source, matched exactly, so a typo
// is reported and never guessed at, unless the table reads it as a reference
// (scheme).
func (r *Registry) requirement(c lowering.Ctx, req *soa.SecurityRequirement, pointer jsontext.Pointer,
) (ir.AuthRequirement, []ir.Diagnostic) {
	var uses []ir.SchemeUse
	var diags []ir.Diagnostic
	for key, member := range req.GetCore().All() {
		id, schemeDiags := r.scheme(c, key, pointer)
		diags = append(diags, schemeDiags...)
		written, _ := nodeview.Strings(member.ValueNode)
		scopes, scopeDiags := requiredScopes(c, written, key, pointer)
		diags = append(diags, scopeDiags...)
		uses = append(uses, ir.SchemeUse{Scheme: id, Scopes: scopes})
	}
	return ir.AuthRequirement{Schemes: uses}, diags
}

// scheme returns the ID of the scheme key names in a requirement c lowers at
// pointer, and what reading it reported. Where the document's minor reads a key
// as a URI and it names no entry of the source, the table reads it as a
// reference, against the document writing it (bind.Config.KeysByURI); any other
// key is the name of an entry of the source.
func (r *Registry) scheme(c lowering.Ctx, key string, pointer jsontext.Pointer) (ir.AuthID, []ir.Diagnostic) {
	if t := c.Bindings().Table(); t != nil {
		if ref, isRef := t.Reference(c.Position(pointer.AppendToken(key)), bind.RefSecurityScheme); isRef {
			return r.referenced(c, key, ref, pointer)
		}
	}
	return r.named(c, key, key, pointer, "")
}

// named returns the ID of the entry of the source called name, which the key
// a requirement writes at pointer names, and reports a name reaching no
// mechanism, saying why where the key is a reference that lands nowhere.
// "Unresolved", not "undeclared": the name may be declared as a $ref resolving
// to nothing, or as an entry naming no mechanism (#294).
func (r *Registry) named(c lowering.Ctx, key, name string, pointer jsontext.Pointer, why string,
) (ir.AuthID, []ir.Diagnostic) {
	id := ids.Auth(name)
	if _, declared := r.byID[id]; declared {
		return id, nil
	}
	id, near := r.unresolved(ids.UnresolvedAuth(name), name)
	return id, []ir.Diagnostic{unresolvedDiag(c, pointer, key, unresolvedClauses(near, why))}
}

// referenced returns the ID of the scheme ref, the reference the key a
// requirement writes at pointer is read as, lands on: an entry of the source as
// that entry's, and any other Security Scheme Object lowered once where it is
// declared (landed). One landing on none is minted from the key and the
// document writing it, so one key in two documents is two schemes.
func (r *Registry) referenced(c lowering.Ctx, key string, ref bind.Reference, pointer jsontext.Pointer,
) (ir.AuthID, []ir.Diagnostic) {
	if ref.Chain.Status != bind.Resolved {
		if name, ok := sourceEntry(c, key); ok {
			return r.named(c, key, name, pointer, whyNot(ref))
		}
		id, near := r.unresolved(c.IDs().UnresolvedAuth(key), key)
		return id, []ir.Diagnostic{unresolvedDiag(c, pointer, key, unresolvedClauses(near, whyNot(ref)))}
	}
	end := ref.Chain.End
	if kind, name, ok := ids.ComponentEntry(end.Pointer); ok && end.Doc == bind.Source && kind == "securitySchemes" {
		return r.named(c, key, name, pointer, "")
	}
	return r.landed(c, end)
}

// sourceEntry returns the name of the entry of the source's securitySchemes
// key names, read as a reference against the document c reads, declared or
// not, so a URI and the name it spells are one scheme.
func sourceEntry(c lowering.Ctx, key string) (string, bool) {
	pointer, ok := c.RefScope().InternalPointer(key)
	if !ok {
		return "", false
	}
	kind, name, ok := ids.ComponentEntry(pointer)
	return name, ok && kind == "securitySchemes"
}

// whyNot says why ref, a requirement key's reference whose chain does not
// resolve, names no Security Scheme Object.
func whyNot(ref bind.Reference) string {
	if ref.Hop.Status != bind.Resolved {
		return ref.Hop.Why()
	}
	return "the chain of references it starts ends on no Security Scheme Object"
}

// landed returns the ID of the Security Scheme Object at end, a position the
// bindings hold one at outside the source's securitySchemes, lowering it in its
// own document the first time a requirement reaches it, so which reaches it
// first decides nothing.
func (r *Registry) landed(c lowering.Ctx, end bind.Position) (ir.AuthID, []ir.Diagnostic) {
	dc, read := lowering.InDocument(c, end.Doc)
	id := dc.IDs().Auth(end.Pointer)
	if _, lowered := r.byID[id]; lowered {
		return id, nil
	}
	rs, _ := c.Bindings().ObjectAt(end, bind.RefSecurityScheme).(*soa.ReferencedSecurityScheme)
	ss := resolve.Object[soa.SecurityScheme](c.Bindings(), rs)
	if !read || ss == nil {
		// The load phase reports a landing it could build nothing at.
		minted, _ := r.unresolved(c.IDs().UnresolvedAuth(string(end.Pointer)), string(end.Pointer))
		return minted, nil
	}
	scheme, diags := lowerSecurityScheme(dc, dc, end.Pointer.LastToken(), ss, end.Pointer, end.Pointer)
	scheme.ID = id
	r.byID[id] = scheme
	return id, diags
}

// unresolvedDiag reports, at pointer, the requirement key key naming no scheme
// with a mechanism, with clauses saying more.
func unresolvedDiag(c lowering.Ctx, pointer jsontext.Pointer, key, clauses string) ir.Diagnostic {
	return c.DiagAt(ir.SeverityError, diag.UnresolvedSecurityScheme, pointer,
		"security requirement references unresolved scheme %q: the requirement is kept, and the scheme has no mechanism%s",
		key, clauses)
}

// requiredScopes returns the scopes written, the list an option at pointer
// writes under key, each kept once, at its first occurrence, beside a warning at
// each repeat: the scopes a scheme is required to hold are a set. Scopes compare
// as the text written, and one scheme's list folds apart from another's.
func requiredScopes(c lowering.Ctx, written []string, key string, pointer jsontext.Pointer) ([]string, []ir.Diagnostic) {
	scopes, repeats := lowering.FirstOccurrences(written)
	diags := make([]ir.Diagnostic, 0, len(repeats))
	for _, r := range repeats {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.RepeatedEntry, pointer+ids.Ptr(key, strconv.Itoa(r.At)),
			"scope %q of scheme %q repeats the entry at index %d; it is kept once", written[r.At], key, r.First))
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
