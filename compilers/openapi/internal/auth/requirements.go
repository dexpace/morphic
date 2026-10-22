package auth

import (
	"encoding/json/jsontext"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// LowerSecurityRequirements lowers an OR-of-ANDs security list (ir-design §9)
// declared under base: "" at the root, else an operation's own declaration,
// never its mount. A nil list inherits the enclosing default; an empty one
// stays []. A non-nil list yields one AuthRequirement per surviving option,
// diagnosed at base+/security/<index>; an empty option {} means "no auth is one
// acceptable choice".
//
// If every option of a non-empty list drops, it collapses to nil, with an
// error: the IR cannot say "auth required, scheme undeclared", #14 forbids an
// unbacked AuthID, and nil never turns a demanded requirement into explicitly
// public []. Dropped names stay out of Unmodeled as document defects.
func LowerSecurityRequirements(c lowering.Ctx, reqs []*soa.SecurityRequirement, base jsontext.Pointer) ([]ir.AuthRequirement, []ir.Diagnostic) {
	if reqs == nil {
		return nil, nil
	}
	out := make([]ir.AuthRequirement, 0, len(reqs))
	var diags []ir.Diagnostic
	for i, req := range reqs {
		pointer := base + ids.Ptr("security", strconv.Itoa(i))
		r, ok, reqDiags := lowerSecurityRequirement(c, req, pointer)
		diags = append(diags, reqDiags...)
		if ok {
			out = append(out, r)
		}
	}
	if len(reqs) > 0 && len(out) == 0 {
		// Every option dropped: nil, not [], which would read as explicitly
		// public. The IR cannot say "auth is required but its scheme is
		// undeclared" and #14 forbids minting an AuthID nothing backs, so the
		// carrier inherits the enclosing default, or is unauthenticated where
		// there is none; both misstate the source, but nil is the one spelling
		// that never reduces a demanded requirement to public. Each collapse
		// carries an error diagnostic.
		return nil, diags
	}
	return out, diags
}

// lowerSecurityRequirement lowers one requirement option declared at pointer:
// each member is a scheme reference plus its scopes. ok reports whether the
// option survives.
//
// A requirement is a conjunction, so a member naming a scheme the auth registry
// does not hold invalidates the whole option, which is dropped in full: never a
// dangling AuthID (issue #14), never the empty option, which would read "no
// auth is also fine" (issue #41). An entry that is no object drops for the
// second reason alone (issue #284), with no report of its own: the loader's
// type-mismatch already names it. Every unresolved member is still diagnosed.
func lowerSecurityRequirement(c lowering.Ctx, req *soa.SecurityRequirement, pointer jsontext.Pointer,
) (r ir.AuthRequirement, ok bool, diags []ir.Diagnostic) {
	if !writtenAsObject(req) {
		return ir.AuthRequirement{}, false, nil
	}
	var uses []ir.SchemeUse
	ok = true
	for name, scopes := range req.All() {
		id := ids.Auth(name)
		if !c.DeclaresAuth(id) {
			// "Unresolved", not "undeclared": the name may be declared as a $ref
			// resolving to nothing, or as an entry naming no mechanism (#294).
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.UnresolvedRef, pointer,
				"security requirement references unresolved scheme %q", name))
			ok = false
			continue
		}
		uses = append(uses, ir.SchemeUse{Scheme: id, Scopes: scopes})
	}
	if !ok {
		return ir.AuthRequirement{}, false, diags
	}
	return ir.AuthRequirement{Schemes: uses}, true, diags
}

// writtenAsObject reports whether the document wrote req as an object, the only
// shape a requirement has.
//
// Null, a scalar and a sequence all unmarshal to a requirement with no members,
// like the {} meaning "no auth is one acceptable choice" (issue #284). The node
// it was read from separates them: the marshaller records a root node only for
// a value it could read as a mapping, so {} carries one inline or through an
// alias. Being library behavior, it is tested through compiled documents.
//
// A nil req is unreachable from a parsed document; that guard is for a
// hand-built slice, as in unresolvableSchemeDiags.
func writtenAsObject(req *soa.SecurityRequirement) bool {
	return req != nil && req.GetRootNode() != nil
}
