// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package auth_test

import (
	"encoding/json/jsontext"
	"strings"
	"testing"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/auth"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// TestSecurityRequirement_OneUndeclaredMemberDropsTheWholeOption pins the
// refusal issue #14 exists for, corrected per issue #41. A requirement may name
// any string; only a name the document declares has an AuthID behind it, and
// writing one for a name it does not declare would put a reference into the IR
// that resolves to nothing. But a requirement is a conjunction — "ghost" and
// "key" must both be satisfied — so ghost failing to resolve makes the whole
// option unsatisfiable. Keeping the option with only ghost removed would leave
// behind a requirement for "key" alone, which is not what the source declared.
func TestSecurityRequirement_OneUndeclaredMemberDropsTheWholeOption(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: []
    key: []
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	assert.Nil(t, svc.Auth,
		"the sole option named an AND of ghost+key; ghost failing to resolve drops it whole, key included")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError),
		"the drop is reported exactly once: %+v", diags)
}

// TestSecurityRequirement_EveryUndeclaredMemberOfAnOptionIsReported pins what
// dropping an option whole must not cost: each name that failed is still named,
// so a reader fixing the document sees every scheme to declare, not only the
// first the walk tripped over.
//
// The compiled Auth cannot show this: stopping at the first bad member drops
// the same option and collapses the same list, so only the diagnostics differ.
// Both names are undeclared for that reason, and read in source order, which
// the IR promises.
func TestSecurityRequirement_EveryUndeclaredMemberOfAnOptionIsReported(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghostA: []
    ghostB: []
paths: {}
`)
	assert.Nil(t, svc.Auth, "neither name resolves, so the sole option drops and the list collapses")
	reported := messagesAt(diags, diag.UnresolvedRef)
	require.Len(t, reported, 2, "both undeclared names are reported, not just the first: %+v", diags)
	assert.Contains(t, reported[0], `"ghostA"`)
	assert.Contains(t, reported[1], `"ghostB"`, "reported in the order the source names them")
	assert.Equal(t, []string{"/security/0", "/security/0"}, sortedPointersAt(diags, diag.UnresolvedRef),
		"each names the option that declared it, which both members share")
}

// TestSecurityRequirement_ADeclaredSchemeIsNeverCalledUndeclared pins that the
// two reports about one broken scheme agree with each other. The document does
// declare "ghost" — its $ref is what fails — so the requirement naming it must
// not be told the scheme is undeclared, which contradicts the load phase's own
// report standing right beside it and sends a reader to add a declaration
// already there. The entry-level report is now the load phase's, naming the
// reference at the entry's own pointer (GitHub #385); the requirement-level one
// still says only that the name is unresolved, never that it was never
// declared.
func TestSecurityRequirement_ADeclaredSchemeIsNeverCalledUndeclared(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - ghost: []
paths: {}
components:
  securitySchemes:
    ghost: {$ref: '#/components/securitySchemes/Missing'}
`)
	assert.Nil(t, svc.Auth, "the sole option names a scheme that resolves to nothing")

	entry := messagesAtPointer(diags, "/components/securitySchemes/ghost")
	require.Len(t, entry, 1, "the entry whose $ref failed is reported: %+v", diags)
	assert.Contains(t, entry[0], `unresolved $ref "#/components/securitySchemes/Missing"`,
		"the load phase's own report, naming the reference that failed")

	req := messagesAtPointer(diags, "/security/0")
	require.Len(t, req, 1, "so is the requirement that names it: %+v", diags)
	assert.Contains(t, req[0], "unresolved", "which is true of a broken $ref and a typo alike")
	assert.NotContains(t, req[0], "undeclared",
		"the document declares this scheme; only its $ref is broken")
}

// TestSecurityRequirement_SoleOptionCollapsesListToNil reproduces issue #41
// directly: a document-level security list whose only option names an
// undeclared scheme must not leave behind AuthRequirement{Schemes: nil}, which
// ir-design §9 reads as "no auth is one acceptable choice" — the opposite of
// what a demanded-but-undeclared scheme means. Nor may it surface as [], which
// ir-design §9 reserves for a deliberate "explicitly public" declaration
// (Service.Auth's doc comment). The list collapses to nil instead: no usable
// default was declared, so operations fall back exactly as if security had
// never been written.
func TestSecurityRequirement_SoleOptionCollapsesListToNil(t *testing.T) {
	t.Parallel()
	doc, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - missing: []
paths: {}
`)
	assert.Nil(t, svc.Auth, "the only option is broken; the list is not left as [{}] or as []")
	assert.Empty(t, doc.Auth, "no scheme is declared at all")
	d, ok := openapitest.FirstDiag(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/security/0"), d.Provenance.Pointer,
		"points at the requirement that named the missing scheme, not a nonexistent components entry")
}

// TestSecurityRequirement_PartialListDropOnlyRemovesTheBrokenOption pins that a
// broken option is dropped in isolation: surviving options keep their source
// order, and a genuinely declared empty option ("no auth is also fine") is
// untouched by the drop of an unrelated, broken option.
//
// The broken option sits at index 1 on purpose. A diagnostic that named the
// list rather than the option inside it — or that spelled every option's index
// as 0 — would send a reader to a requirement that is perfectly valid, so the
// index is load-bearing and only a non-zero one can show that it is carried.
func TestSecurityRequirement_PartialListDropOnlyRemovesTheBrokenOption(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security:
  - key: []
  - missing: []
  - {}
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	require.Len(t, svc.Auth, 2, "only the middle, broken option drops")
	require.Len(t, svc.Auth[0].Schemes, 1)
	assert.Equal(t, ids.Auth("key"), svc.Auth[0].Schemes[0].Scheme, "the first option survives in place")
	assert.Empty(t, svc.Auth[1].Schemes, "the trailing empty option still means no-auth-is-fine")
	assert.Equal(t, 1, openapitest.CountDiagsAt(diags, diag.UnresolvedRef, ir.SeverityError))
	d, ok := openapitest.FirstDiag(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/security/1"), d.Provenance.Pointer,
		"the pointer names the broken option's own index, not the list or a constant 0")
}

// TestSecurityRequirement_OperationLevelSoleOptionCollapsesToNil is the
// operation-level counterpart of TestSecurityRequirement_SoleOptionCollapsesListToNil:
// an operation's own security override degrades the same way the service
// default does — collapsing to nil, so the operation falls back to inheriting
// the service default rather than reading as explicitly public — and its
// diagnostic points at the operation's own declaration site rather than the
// document root or a nonexistent components entry.
func TestSecurityRequirement_OperationLevelSoleOptionCollapsesToNil(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
      security: [{missing: []}]
      responses: {"200": {description: ok}}
`)
	require.NotEmpty(t, svc.Groups, "the operation was lowered into a group")
	require.NotEmpty(t, svc.Groups[0].Operations, "the operation was lowered")
	op := svc.Groups[0].Operations[0]
	assert.Nil(t, op.Auth, "the operation's sole option is broken; it now inherits the service default")
	d, ok := openapitest.FirstDiag(diags, diag.UnresolvedRef)
	require.True(t, ok, "an unresolved-ref diagnostic: %+v", diags)
	assert.Equal(t, jsontext.Pointer("/paths/~1x/get/security/0"), d.Provenance.Pointer,
		"points at the operation's own requirement, not a nonexistent components entry")
}

// TestSecurityRequirement_EveryOperationCarrierDiagnosesAtItsDeclaration pins
// the base pointer for the carriers an operation-level security list sits on
// besides an inline path operation. All reach one LowerSecurityRequirements
// call, so one wrong argument misplaces every one of their diagnostics. An
// inline path operation cannot show that: it is mounted where it is declared,
// so the two pointers are equal. A $ref'd path item separates them, mounted
// under /paths and declared under /components, where the list is written. Its
// broken option sits at index 1 so the survivor is kept too.
//
// The pointers are compared as a sorted set, since carrier order is not the
// claim.
func TestSecurityRequirement_EveryOperationCarrierDiagnosesAtItsDeclaration(t *testing.T) {
	t.Parallel()
	_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /cb:
    post:
      operationId: cbOp
      responses: {"200": {description: ok}}
      callbacks:
        onEvent:
          '{$request.body#/url}':
            post:
              operationId: callbackOp
              security: [{missing: []}]
              responses: {"200": {description: ok}}
  /reffed:
    $ref: '#/components/pathItems/shared'
webhooks:
  hook:
    post:
      operationId: hookOp
      security: [{missing: []}]
      responses: {"200": {description: ok}}
components:
  pathItems:
    shared:
      get:
        operationId: sharedOp
        security: [{key: []}, {missing: []}]
        responses: {"200": {description: ok}}
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
	want := []string{
		"/components/pathItems/shared/get/security/1",
		"/paths/~1cb/post/callbacks/onEvent/{$request.body#~1url}/post/security/0",
		"/webhooks/hook/post/security/0",
	}
	assert.Equal(t, want, sortedPointersAt(diags, diag.UnresolvedRef),
		"each carrier reports at its own declaration site")

	// Each lookup is required to hit before it is asserted on: a missing key
	// yields the zero Operation, whose Auth is nil, so an absent carrier would
	// satisfy the nil assertions below without ever having been compiled.
	ops := operationsByDeclaration(svc)
	require.Len(t, ops, len(want)+1, "one operation per carrier, plus the callback's own parent")
	hook, ok := ops["/webhooks/hook/post"]
	require.True(t, ok, "the webhook operation was lowered")
	assert.Nil(t, hook.Auth, "the webhook's sole option is broken, so it inherits")
	callback, ok := ops["/paths/~1cb/post/callbacks/onEvent/{$request.body#~1url}/post"]
	require.True(t, ok, "the callback operation was lowered")
	assert.Nil(t, callback.Auth, "the callback operation's sole option is broken, so it inherits")
	shared, ok := ops["/components/pathItems/shared/get"]
	require.True(t, ok, "the $ref'd path item's operation was lowered")
	require.Len(t, shared.Auth, 1, "it keeps its one good option")
	require.Len(t, shared.Auth[0].Schemes, 1)
	assert.Equal(t, ids.Auth("key"), shared.Auth[0].Schemes[0].Scheme)
}

// operationsByDeclaration indexes every operation in svc by its provenance
// pointer, which is where the operation is declared rather than where it mounts.
func operationsByDeclaration(svc ir.Service) map[string]ir.Operation {
	out := make(map[string]ir.Operation)
	for _, g := range svc.Groups {
		for _, op := range g.Operations {
			out[string(op.Provenance.Pointer)] = op
		}
	}
	return out
}

// TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne pins the difference
// between a document that says nothing about security and one that says
// explicitly that none is required.
//
// ir.Service.Auth and ir.Operation.Auth are omitzero, so the IR spells the two
// apart — an absent key for "inherit whatever encloses this", [] for
// "explicitly public" (ir-design §9). Collapsing them makes an explicitly public
// operation read as one that never mentioned security, which is the opposite
// claim.
func TestSecurityRequirements_AnEmptyListIsNotAnAbsentOne(t *testing.T) {
	t.Parallel()
	var c lowering.Ctx

	absent, absentDiags := auth.LowerSecurityRequirements(c, nil, "")
	assert.Nil(t, absent, "no security key at all inherits the enclosing default")
	assert.Empty(t, absentDiags)

	empty, emptyDiags := auth.LowerSecurityRequirements(c, []*soa.SecurityRequirement{}, "")
	assert.NotNil(t, empty, "an empty list is a declaration, not an absence")
	assert.Empty(t, empty, "and it declares no options")
	assert.Empty(t, emptyDiags)
}

// TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption pins that a
// security-list entry not written as an object is dropped whole rather than
// lowered to AuthRequirement{}, which ir-design §9 reserves for "no auth is one
// acceptable choice" and which, beside a real requirement, would make auth
// optional (GitHub #284).
//
// The `{}` an author writes on purpose is a row too: refusing every memberless
// option would pass the malformed rows while deleting that encoding.
//
// Each malformed row pins that the loader's report survives, since the drop is
// otherwise silent. The aliased rows catch a drop that tests the entry's node
// kind, which would lose the `{}` reached through an alias.
func TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry string
		// wantOptions is how many of the two declared options survive.
		wantOptions int
	}{
		{name: "null", entry: `null`, wantOptions: 1},
		{name: "YAML's other spelling of null", entry: `~`, wantOptions: 1},
		{name: "a string", entry: `"key"`, wantOptions: 1},
		{name: "a number", entry: `42`, wantOptions: 1},
		{name: "a sequence", entry: `[key]`, wantOptions: 1},
		{name: "an alias to a scalar", entry: `*scalar`, wantOptions: 1},
		{name: "an option the document really wrote as {}", entry: `{}`, wantOptions: 2},
		{name: "an option reached through an alias to {}", entry: `*empty`, wantOptions: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
x-defs:
  empty: &empty {}
  scalar: &scalar just a string
security:
  - key: []
  - `+tc.entry+`
paths: {}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
			require.Len(t, svc.Auth, tc.wantOptions, "surviving options: %+v", svc.Auth)
			require.Len(t, svc.Auth[0].Schemes, 1, "the real requirement is untouched")
			assert.Equal(t, ids.Auth("key"), svc.Auth[0].Schemes[0].Scheme)

			if tc.wantOptions == 2 {
				assert.Empty(t, svc.Auth[1].Schemes, "the empty option the document wrote is kept")
				assert.Empty(t, loaderErrors(diags), "a document with nothing wrong with it: %+v", diags)
				return
			}
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "/security/1",
				"the entry is dropped here without a report, so the loader's must place it, or name where an "+
					"alias reads it: %+v", diags)
		})
	}
}

// TestSecurityRequirement_OperationLevelNonObjectEntryIsNotTheEmptyOption is the
// operation-level counterpart of TestSecurityRequirement_ANonObjectEntryIsNotTheEmptyOption.
// Both levels reach one lowering, but only a test at each proves it: a
// document-level fixture alone passes just as well on a fix wired into the
// service walk and never reached by an operation's own override.
func TestSecurityRequirement_OperationLevelNonObjectEntryIsNotTheEmptyOption(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		entry       string
		wantOptions int
	}{
		{name: "null", entry: `null`, wantOptions: 1},
		{name: "an option the operation really wrote as {}", entry: `{}`, wantOptions: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, svc, diags := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
  /x:
    get:
      operationId: x
      security:
        - key: []
        - `+tc.entry+`
      responses: {"200": {description: ok}}
components:
  securitySchemes:
    key: {type: apiKey, in: header, name: X-Key}
`)
			op := onlyOperation(t, svc)
			require.Len(t, op.Auth, tc.wantOptions, "surviving options: %+v", op.Auth)
			require.Len(t, op.Auth[0].Schemes, 1, "the real requirement is untouched")
			assert.Equal(t, ids.Auth("key"), op.Auth[0].Schemes[0].Scheme)

			if tc.wantOptions == 2 {
				assert.Empty(t, op.Auth[1].Schemes, "the empty option the operation wrote is kept")
				return
			}
			assert.Contains(t, strings.Join(loaderErrors(diags), "\n"), "/paths/~1x/get/security/1: ",
				"the entry is dropped here without a report, so the loader's must place it: %+v", diags)
		})
	}
}

// TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil pins the
// solo spelling at both levels. It is the worse half of #284: `security: [null]`
// lowered to [{}], which ir-design §9 reads as an explicit "no auth is one
// acceptable choice" — a document that says nothing usable about security
// instead declaring the API public. Dropping the entry leaves an originally
// non-empty list empty, which the enclosing collapse turns into nil, so the
// carrier inherits exactly as if security had never been written.
func TestSecurityRequirement_ASoleNonObjectEntryCollapsesTheListToNil(t *testing.T) {
	t.Parallel()
	_, svc, _ := serviceSpec(t, `openapi: 3.1.0
info: {title: T, version: "1"}
security: [null]
paths:
  /x:
    get:
      operationId: x
      security: [null]
      responses: {"200": {description: ok}}
`)
	assert.Nil(t, svc.Auth, "the document's sole option is not an option; the list is not [{}] or []")
	assert.Nil(t, onlyOperation(t, svc).Auth, "nor is the operation's own override")
}

// onlyOperation returns svc's single operation, failing if it has any other
// number — an assertion the callers need before reading Auth, because a missing
// operation yields the zero value, whose Auth is nil and would satisfy a nil
// assertion without anything having been compiled.
func onlyOperation(t *testing.T, svc ir.Service) ir.Operation {
	t.Helper()
	var ops []ir.Operation
	for _, g := range svc.Groups {
		ops = append(ops, g.Operations...)
	}
	require.Len(t, ops, 1, "the fixture declares exactly one operation")
	return ops[0]
}

// loaderErrors returns every error the load phase's validation produced, in
// report order, as its pointer and message. It matches on the code prefix every
// such finding carries rather than on one rule, so a test asserts where the
// entry was placed without pinning which rule placed it.
func loaderErrors(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == ir.SeverityError && strings.HasPrefix(d.Code, diag.Validation+"/") {
			out = append(out, string(d.Provenance.Pointer)+": "+d.Message)
		}
	}
	return out
}

// TestLowerSecurityRequirements_DropsANilEntrySilently covers the half of the
// object guard a parsed document cannot reach: a hand-built list holding a nil
// entry. It is dropped like any other entry that is no object, rather than
// surviving as the empty option that reads as "no auth is one acceptable
// choice", and silently: an entry that is no object is the loader's to report,
// and a nil one is in no document at all.
func TestLowerSecurityRequirements_DropsANilEntrySilently(t *testing.T) {
	t.Parallel()
	got, diags := auth.LowerSecurityRequirements(lowering.Ctx{}, []*soa.SecurityRequirement{nil}, "/security")

	assert.Empty(t, got, "a nil entry is no requirement, so it is dropped")
	assert.Empty(t, diags)
}
