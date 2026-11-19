// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/compilers/openapi/internal/service"
	"github.com/dexpace/morphic/compilers/openapi/internal/service/servicetest"
)

// TestDeadBranch_ReadsANullWhereTheIRHoldsIt holds the dead-branch check to the
// one reading of null (mayAdmitNull): a branch naming a union admits the null
// a variant of it admits, declared or read in by the nullable-inference
// policy; a body admits the null all it writes but the union does; and a body
// whose read passes the walk's budget may admit null unless what it read keeps
// null out (docs/compilers/openapi.md#union-beside-structure).
func TestDeadBranch_ReadsANullWhereTheIRHoldsIt(t *testing.T) {
	t.Parallel()
	serviceContext := servicetest.Context(t, openapitest.ComponentSpecVer("3.1.0", deadNullSpec()), service.Policy{})
	for name, dead := range map[string]bool{
		"OnAVariant": false, "OnALenientVariant": false, "BesideNoNull": true,
		"PastTheWalk": false, "KeptOutPastTheWalk": true,
	} {
		st := heldAt(serviceContext.Ctx, jsontext.Pointer("/components/schemas/"+name))
		assert.Equal(t, []bool{dead, false}, deadBranches(serviceContext.Ctx, st.Schema, st.Schema), name)
	}
}

// deadNullSpec declares bodies beside a union of a branch admitting strings
// and perhaps null, and an object: over a union of a string and a $ref to
// null, over one whose second branch admits null only by the policy's reading
// of nullable, which 3.1 does not define, and an allOf chain the walk cannot
// finish, ending on an object alone, beside no type or one keeping null out.
func deadNullSpec() string {
	ref := func(name string) string { return "{$ref: '#/components/schemas/" + name + "'}" }
	var b strings.Builder
	b.WriteString("    S0: {type: string}\n    NUL: {type: 'null'}\n    O: {type: object}\n")
	b.WriteString("    T: {anyOf: [" + ref("S0") + ", " + ref("NUL") + "]}\n")
	b.WriteString("    L: {anyOf: [" + ref("S0") + ", {type: string, nullable: true}]}\n")
	union := func(t string) string {
		return "properties: {u: {type: string}}, anyOf: [" + ref(t) + ", " + ref("O") + "]"
	}
	fmt.Fprintf(&b, "    OnAVariant: {type: [object, 'null'], %s}\n", union("T"))
	fmt.Fprintf(&b, "    OnALenientVariant: {type: [object, 'null'], %s}\n", union("L"))
	fmt.Fprintf(&b, "    BesideNoNull: {type: object, %s}\n", union("T"))
	fmt.Fprintf(&b, "    PastTheWalk: {allOf: [%s], %s}\n", ref("B0"), union("T"))
	fmt.Fprintf(&b, "    KeptOutPastTheWalk: {type: object, allOf: [%s], %s}\n", ref("B0"), union("T"))
	for i := range maxNullConjuncts {
		fmt.Fprintf(&b, "    B%d: {allOf: [%s]}\n", i, ref(fmt.Sprintf("B%d", i+1)))
	}
	fmt.Fprintf(&b, "    B%d: {type: object}\n", maxNullConjuncts)
	return b.String()
}
