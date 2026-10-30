// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irtest"
	"github.com/dexpace/morphic/ir/irverify"
)

// emptySHA256 is the SHA-256 of zero bytes in lowercase hex, a digest of the
// shape every SourceInfo.Hash has.
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// fixtureSpace is the namespace the service verifyDeclared adds lives in, the
// one the fixtures' scaffolding IDs share.
const fixtureSpace = "x"

// oneSource is the table of a document compiled from one file and nothing
// else, so index 0 is the only addressable source.
func oneSource() []ir.SourceInfo {
	return []ir.SourceInfo{{Format: "openapi@3.1", Path: "api.yaml", Hash: emptySHA256}}
}

// fixtureService is the smallest service that draws no violation: a valid ID in
// space and a neutral name.
func fixtureService(space string) ir.Service {
	return ir.Service{
		ID:   ir.ServiceID(ir.IDKindService + ir.IDSeparator + space + ir.IDSeparator + "S"),
		Name: named("svc"),
	}
}

// verifyDeclared runs Verify on a copy of doc that declares what a compiled
// document always carries and doc leaves nil: one source, one service and the
// namespaces its IDs live in.
//
// The fixture stamps an irVersion itself; this does the same for those tables,
// so a test about some other rule is not also a test of them. Tests of a table
// build their own and call irverify.Verify directly. A declaration the fixture
// sets is kept, gaining the service's kind on a copy when it has none.
func verifyDeclared(doc *ir.Document) []irverify.Violation {
	if doc == nil {
		return irverify.Verify(doc)
	}
	declared := *doc
	if declared.Sources == nil {
		declared.Sources = oneSource()
	}
	if declared.Services == nil {
		space := serviceSpace(declared.IDSpaces)
		declared.Services = []ir.Service{fixtureService(space)}
		declared.IDSpaces = declaringService(declared.IDSpaces, space)
	}
	if declared.IDSpaces == nil {
		declared.IDSpaces = irtest.SpacesUsed(&declared)
	}
	return irverify.Verify(&declared)
}

// serviceSpace returns the namespace a service added to a document declaring
// spaces is minted in: the first the declaration lists for services, else the
// fixtures' shared one.
func serviceSpace(spaces map[string][]string) string {
	if listed := spaces[ir.IDKindService]; len(listed) > 0 {
		return listed[0]
	}
	return fixtureSpace
}

// declaringService returns spaces with space declared for services, so the
// service verifyDeclared added draws no undeclared-namespace violation. A nil
// declaration stays nil, for irtest.SpacesUsed to fill, and the fixture's own
// map is never written.
func declaringService(spaces map[string][]string, space string) map[string][]string {
	if spaces == nil {
		return nil
	}
	if _, listed := spaces[ir.IDKindService]; listed {
		return spaces
	}
	out := maps.Clone(spaces)
	out[ir.IDKindService] = []string{space}
	return out
}

// TestVerifyDeclared_KeepsTheDeclarationTheFixtureSets pins the helper's one
// branching decision: the service it adds to a fixture declaring its own
// namespaces lives in one of them, so it draws no undeclared-namespace
// violation, and the fixture's map is never written.
func TestVerifyDeclared_KeepsTheDeclarationTheFixtureSets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		spaces map[string][]string
	}{
		{"none for services", map[string][]string{ir.IDKindType: {"x"}}},
		{"another for services", map[string][]string{ir.IDKindType: {"x"}, ir.IDKindService: {"other"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := maps.Clone(tc.spaces)

			assert.Empty(t, verifyDeclared(spacedDoc(tc.spaces)))
			assert.Equal(t, before, tc.spaces)
		})
	}
}
