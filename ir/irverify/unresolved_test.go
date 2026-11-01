// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/ir"
)

const unresolvedID ir.AuthID = "auth/unresolved/components/securitySchemes/token"

// schemeDoc interns scheme under its own ID, so nothing but what the scheme says
// is open to question.
func schemeDoc(scheme ir.AuthScheme) *ir.Document {
	return &ir.Document{IRVersion: ir.IRVersion, Auth: map[ir.AuthID]ir.AuthScheme{scheme.ID: scheme}}
}

// TestVerify_AnUnresolvedSchemeInItsSpaceIsClean holds the silent half of both
// rules: what the rules allow beside a name must verify.
func TestVerify_AnUnresolvedSchemeInItsSpaceIsClean(t *testing.T) {
	t.Parallel()
	scheme := ir.AuthScheme{
		ID: unresolvedID, Name: named("token"), Kind: ir.AuthKindUnresolved,
		Docs:        ir.Docs{Description: "Issued by the upload endpoint."},
		Deprecation: &ir.Deprecation{},
		Unmodeled: ir.Unmodeled{"openapi:in": ir.UnmodeledEntry{
			Reason: ir.ReasonDegradedLowering, Value: ir.RawValue(`"header"`),
			Provenance: ir.Provenance{Source: ir.NoSource},
		}},
		Provenance: ir.Provenance{Source: ir.NoSource},
	}

	assert.Empty(t, verifyDeclared(schemeDoc(scheme)))
}

// TestVerify_AnUnresolvedSchemeLivesInItsOwnSpace pins invariant 3 for a
// document no compiler here produced: a kind and a space that disagree let a
// reference spelled at a pointer reach a node with no mechanism, or a mechanism
// hide in the space an emitter reads as having none.
func TestVerify_AnUnresolvedSchemeLivesInItsOwnSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   ir.AuthID
		kind ir.AuthKind
	}{
		{name: "the kind outside the space", id: "auth/openapi/components/securitySchemes/token", kind: ir.AuthKindUnresolved},
		{name: "a mechanism inside the space", id: unresolvedID, kind: ir.AuthKindAPIKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scheme := ir.AuthScheme{ID: tc.id, Name: named("token"), Kind: tc.kind,
				Provenance: ir.Provenance{Source: ir.NoSource}}
			got := verifyDeclared(schemeDoc(scheme))
			require.Equal(t, []string{"ir/unresolved-auth-space"}, codesOf(got))
			assert.Equal(t, "auth["+string(tc.id)+"]", got[0].Path)
		})
	}
}

// TestVerify_AMalformedIDIsNotASpaceFinding pins that a scheme whose ID has no
// space draws the malformed-ID report alone: there is no space to disagree with.
func TestVerify_AMalformedIDIsNotASpaceFinding(t *testing.T) {
	t.Parallel()
	const id ir.AuthID = "auth/"
	got := verifyDeclared(schemeDoc(ir.AuthScheme{ID: id, Name: named("token"),
		Kind: ir.AuthKindUnresolved, Provenance: ir.Provenance{Source: ir.NoSource}}))

	assert.Equal(t, []string{"ir/id-malformed"}, codesOf(got))
}

// nonZero returns a value of typ no emptiness test takes for empty.
func nonZero(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()
	switch typ.Kind() {
	case reflect.String:
		return reflect.ValueOf("x").Convert(typ)
	case reflect.Slice:
		return reflect.MakeSlice(typ, 1, 1)
	case reflect.Pointer:
		return reflect.New(typ.Elem())
	default:
		require.Failf(t, "unhandled field", "add a value of kind %s", typ.Kind())
		return reflect.Value{}
	}
}

// TestVerify_AnUnresolvedSchemeStatesNoMechanism sets every field of
// ir.AuthScheme alone on an unresolved scheme, so a field added to the struct
// fails here until it is classified, rather than going unread by the predicate.
// A name and text about the scheme are what the kind leaves; anything an SDK
// would authenticate with is what it denies.
func TestVerify_AnUnresolvedSchemeStatesNoMechanism(t *testing.T) {
	t.Parallel()
	about := map[string]bool{
		"ID": true, "Name": true, "Kind": true, "Docs": true, "Deprecation": true,
		"Unmodeled": true, "Provenance": true,
	}
	typ := reflect.TypeFor[ir.AuthScheme]()
	for i := range typ.NumField() {
		field := typ.Field(i)
		if about[field.Name] {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			t.Parallel()
			scheme := ir.AuthScheme{ID: unresolvedID, Name: named("token"), Kind: ir.AuthKindUnresolved,
				Provenance: ir.Provenance{Source: ir.NoSource}}
			reflect.ValueOf(&scheme).Elem().Field(i).Set(nonZero(t, field.Type))

			assert.Contains(t, codesOf(verifyDeclared(schemeDoc(scheme))), "ir/unresolved-auth-has-mechanism")
		})
	}
}
