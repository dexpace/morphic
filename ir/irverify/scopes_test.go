package irverify_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
)

// scopesDoc interns one sound oauth2 scheme holding flows, so nothing but their
// scopes is open to question.
func scopesDoc(flows ...ir.OAuthFlow) *ir.Document {
	doc := authDoc(ir.AuthKindOAuth2)
	for id, scheme := range doc.Auth {
		scheme.Flows = flows
		doc.Auth[id] = scheme
	}
	return doc
}

// TestVerify_ARepeatedOAuthScopeIsAViolation pins that a flow offers each scope
// name once, whatever the descriptions say: each repeat is reported at its own
// entry, after the first. A name may recur in another flow, and a list of
// distinct names, the empty one among them, is clean.
func TestVerify_ARepeatedOAuthScopeIsAViolation(t *testing.T) {
	t.Parallel()
	const scheme = "auth[auth/openapi/components/securitySchemes/token]"
	flow := func(names ...string) ir.OAuthFlow {
		fl := ir.OAuthFlow{Kind: "client_credentials", TokenURL: "https://a.example/t"}
		for i, name := range names {
			fl.Scopes = append(fl.Scopes, ir.OAuthScope{Name: name, Description: string(rune('A' + i))})
		}
		return fl
	}
	tests := []struct {
		name  string
		flows []ir.OAuthFlow
		paths []string
	}{
		{"a repeat after another scope", []ir.OAuthFlow{flow("read", "write", "read")},
			[]string{scheme + ".Flows[0].Scopes[2]"}},
		{"a name written three times", []ir.OAuthFlow{flow("read", "read", "read")},
			[]string{scheme + ".Flows[0].Scopes[1]", scheme + ".Flows[0].Scopes[2]"}},
		{"a repeat in the second flow", []ir.OAuthFlow{flow("read"), flow("write", "write")},
			[]string{scheme + ".Flows[1].Scopes[1]"}},
		{"one name in two flows", []ir.OAuthFlow{flow("read"), flow("read")}, nil},
		{"distinct names, the empty one among them", []ir.OAuthFlow{flow("", "read", "write")}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := verifyDeclared(scopesDoc(tc.flows...))

			paths := make([]string, 0, len(got))
			for _, v := range got {
				assert.Equal(t, "ir/oauth-scope-duplicate", v.Code)
				paths = append(paths, v.Path)
			}
			assert.Equal(t, tc.paths, nilIfEmpty(paths))
		})
	}
}

// nilIfEmpty returns nil for an empty list, so a clean case compares as nil.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}
