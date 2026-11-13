// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// redact asks for p's redaction where its name says it holds a credential
// (redactUse): a model's member or a header.
func redact(c lowering.Ctx, reg ir.TypeRegistry, p *ir.Property) {
	redactUse(c, reg, p.Name, p.Type, p.Unmodeled, &p.Secret, &p.Provenance)
}

// redactUse sets *secret, and marks prov, under the secret policy, on a use
// named name, of type t, keeping own, where the name's canonical words end in
// a credential's (credentialName) and t reads, through its scalars, as text
// or bytes. It leaves alone a use already redacted, one whose type asks for
// it, and one where the document writes a key the policy reads redaction from,
// at the use or along its type, whatever the key says: the document has spoken
// there. The rule is docs/compilers/openapi.md#secret-inference.
func redactUse(c lowering.Ctx, reg ir.TypeRegistry, name ir.Naming, t ir.TypeRef, own ir.Unmodeled,
	secret *bool, prov *ir.Provenance,
) {
	if !c.SecretInference.Honours() || *secret || !credentialName(name.Canonical) {
		return
	}
	promoter := c.Promoter()
	if promoter.Writes(own, lowering.TargetRedaction) {
		return
	}
	ch, ok := readChain(promoter, reg, t.Target)
	if !ok || ch.sensitive || ch.spoken || ch.prim != ir.PrimString && ch.prim != ir.PrimBytes {
		return
	}
	*secret = true
	prov.Inferred = ir.JoinInferred(prov.Inferred, lowering.SecretInferenceHeuristic)
}

// credentialName reports whether canonical, a name's canonical words, ends in
// a phrase naming a credential, numbers after it aside: a password, a
// passphrase or a secret, an API, access, secret or private key, an access,
// refresh, auth, bearer, session or API token, or, as the whole name, the
// field RFC 9110 defines as credentials, an authorization or a proxy's. A name
// ending in another word, as `secret_id` and `page_token` do, names something
// else, and `token` alone names a credential in one document and a unit of
// text in another.
func credentialName(canonical string) bool {
	rest, last := lastWord(strings.TrimRight(canonical, "_0123456789"))
	_, before := lastWord(rest)
	switch last {
	case "password", "passwd", "passphrase", "secret", "apikey":
		return true
	case "phrase":
		return before == "pass"
	case "authorization":
		return rest == "" || rest == "proxy"
	case "key":
		return before == "api" || before == "access" || before == "secret" || before == "private"
	case "token":
		switch before {
		case "access", "refresh", "auth", "bearer", "session", "api":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// lastWord splits the last of canonical's words from the words before it,
// rest empty where it holds one word.
func lastWord(canonical string) (rest, last string) {
	rest, last, found := strings.CutLast(canonical, "_")
	if !found {
		return "", canonical
	}
	return rest, last
}
