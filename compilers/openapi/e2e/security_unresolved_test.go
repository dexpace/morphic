// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// TestSecurityUnresolved_ANameTwoDocumentsUseIsOneSchemeOfTheSource pins that a
// scheme minted for a name is the source's, whichever document holds a
// requirement naming it: a name is read in the entry document, and the node's
// identity and provenance must not follow the requirement that reached it first.
// Each root declares the two requirements in one of the two orders.
func TestSecurityUnresolved_ANameTwoDocumentsUseIsOneSchemeOfTheSource(t *testing.T) {
	t.Parallel()
	const (
		other = `openapi: 3.1.0
info: {title: O, version: "1"}
paths:
  /there:
    get:
      operationId: there
      security: [{nosuch: [read]}]
      responses: {"200": {description: ok}}
`
		there = `  /there: {$ref: 'other.yaml#/paths/~1there'}
`
		here = `  /here:
    get:
      operationId: here
      security: [{nosuch: []}]
      responses: {"200": {description: ok}}
`
		head = `openapi: 3.1.0
info: {title: T, version: "1"}
paths:
`
		minted ir.AuthID = "auth/unresolved/components/securitySchemes/nosuch"
	)
	for name, paths := range map[string]string{"there first": there + here, "here first": here + there} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			layout(t, dir, map[string]string{"other.yaml": other})

			doc, diags := compileOnDisk(t, filepath.Join(dir, "root.yaml"), head+paths, true)

			require.Greater(t, len(doc.Sources), 1, "the fixture reads a second document: %+v", diags)
			assert.Empty(t, irverify.Verify(doc))
			require.Len(t, doc.Auth, 1, "%+v", doc.Auth)
			assert.Equal(t, ir.Provenance{Source: 0}, doc.Auth[minted].Provenance)
			for _, id := range []string{"here", "there"} {
				op, ok := opByName(doc, id)
				require.True(t, ok, id)
				require.Len(t, op.Auth, 1, id)
				assert.Equal(t, minted, op.Auth[0].Schemes[0].Scheme, id)
			}
		})
	}
}

// TestSecurityUnresolved_AnUndecodedSchemeRefIsReportedByAFullCompile pins that
// the error for a securitySchemes $ref naming nothing reaches the caller when
// the parser's model did not decode the $ref. The load phase then meets no
// reference and reports nothing, so the lowering's report is the only one, and
// a compile dropping the lowering's reports the load phase made already must
// keep it.
func TestSecurityUnresolved_AnUndecodedSchemeRefIsReportedByAFullCompile(t *testing.T) {
	t.Parallel()
	doc, diags := parseFull(t, `openapi: 3.1.0
info: {title: t, version: '1'}
paths: {}
components:
  securitySchemes:
    S: {$ref: !local '#/components/securitySchemes/Ghost'}
`)
	var refs []string
	for _, d := range diags {
		if d.Code == diag.UnresolvedRef {
			refs = append(refs, string(d.Provenance.Pointer))
		}
	}
	assert.Equal(t, []string{"/components/securitySchemes/S"}, refs, "one report, at the entry: %+v", diags)
	assert.Equal(t, []ir.Severity{ir.SeverityError}, diagsAt(diags, diag.UnresolvedRef, "/components/securitySchemes/S"))
	assert.Equal(t, ir.AuthKindUnresolved, doc.Auth["auth/unresolved/components/securitySchemes/S"].Kind,
		"the entry lowers to a scheme of no mechanism: %+v", doc.Auth)
}
