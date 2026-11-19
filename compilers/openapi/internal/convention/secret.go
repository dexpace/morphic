// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package convention

import (
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/ir"
)

// redact asks for property's redaction where its name says it holds a credential
// (redactUse): a model's member or a header.
func redact(policy SecretInference, promoter promotion.Promoter, reg ir.TypeRegistry, property *ir.Property) {
	redactUse(policy, promoter, reg, property.Name, property.Type, property.Unmodeled, &property.Secret, &property.Provenance)
}

// redactUse sets *secret, and marks prov, under the secret policy, on a use
// named name, of type typeRef, keeping own, where the name's canonical words end in
// a credential's (credentialName) and typeRef reads, through its scalars, as text
// or bytes. It leaves alone a use already redacted, one whose type asks for
// it, and one where the document writes a key the policy reads redaction from,
// at the use or along its type, whatever the key says: the document has spoken
// there. The rule is docs/compilers/openapi.md#secret-inference.
func redactUse(policy SecretInference, promoter promotion.Promoter, reg ir.TypeRegistry, name ir.Naming,
	typeRef ir.TypeRef, own ir.Unmodeled, secret *bool, prov *ir.Provenance,
) {
	if !policy.Honours() || *secret || !credentialName(name.Canonical) {
		return
	}
	if promoter.Writes(own, promotion.TargetRedaction) {
		return
	}
	ch, ok := readChain(promoter, reg, typeRef.Target)
	if !ok || ch.sensitive || ch.spoken || ch.prim != ir.PrimitiveString && ch.prim != ir.PrimitiveBytes {
		return
	}
	*secret = true
	prov.Inferred = ir.JoinInferred(prov.Inferred, SecretInferenceHeuristic)
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
