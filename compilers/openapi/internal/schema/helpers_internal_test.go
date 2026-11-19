// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// The tests in this file and beside it are the ones that read an unexported
// lowering directly. Everything reachable through the exported surface lives in
// package schema_test, which can import the whole compiler and drive a spec end
// to end; nothing importable from there would reach these.

// deepPointer is a sub-schema pointer (not a top-level component pointer), so it
// exercises the interning-lookup paths rather than the named-component path.
const deepPointer = "/components/schemas/Obj/properties/inner"

// assertInternalInvariant requires diags to report a broken internal invariant.
// It is the only diagnostic this half of the suite asserts by code, and that is
// not a coincidence: these tests drive the states no source document can reach,
// which is exactly what that code is for.
func assertInternalInvariant(t *testing.T, diags []ir.Diagnostic) {
	t.Helper()
	for _, diagnostic := range diags {
		if diagnostic.Code == diag.InternalInvariant && diagnostic.Severity == ir.SeverityError {
			return
		}
	}
	t.Fatalf("expected an internal-invariant error, got %+v", diags)
}
