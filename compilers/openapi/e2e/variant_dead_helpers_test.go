// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"encoding/json/jsontext"
	"fmt"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// liveBranches returns the branches of a union distributed over an object body
// that some instance satisfies, in order: the ones its variants stand for.
func liveBranches(branches []variantBranch) []variantBranch {
	var out []variantBranch
	for _, b := range branches {
		if b.object {
			out = append(out, b)
		}
	}
	return out
}

// deadKeys returns the key under which the union keeps each branch no instance
// of the object body satisfies: its keyword and its index among branches.
func deadKeys(keyword string, branches []variantBranch) []string {
	var out []string
	for i, b := range branches {
		if !b.object {
			out = append(out, fmt.Sprintf("openapi:%s/%d", keyword, i))
		}
	}
	return out
}

// deadWarnings returns the one warning each of conjunctionPositions draws
// where some branch of its union admits no object, naming each such branch.
func deadWarnings(keyword string, branches []variantBranch) []wantFinding {
	var named []string
	for i, b := range branches {
		if !b.object {
			named = append(named, fmt.Sprintf("%s/%d: ", keyword, i))
		}
	}
	if len(named) == 0 {
		return nil
	}
	out := make([]wantFinding, 0, len(conjunctionPositions))
	for _, at := range conjunctionPositions {
		out = append(out, wantFinding{code: diag.DegradedConstruct, pointer: string(at), contains: named})
	}
	return out
}

// countFindings counts the findings of diags coded code, of the severity
// given, at the pointer given.
func countFindings(diags []ir.Diagnostic, code string, severity ir.Severity, at jsontext.Pointer) int {
	n := 0
	for _, diagnostic := range diags {
		if diagnostic.Code == code && diagnostic.Severity == severity && diagnostic.Provenance.Pointer == at {
			n++
		}
	}
	return n
}
