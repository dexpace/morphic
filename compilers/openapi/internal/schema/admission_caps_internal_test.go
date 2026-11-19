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

// TestAdmission_ACapStopsTheElectionsSummaryAlone pins what each kind of read
// leaves its readers: one that finishes, one a cycle of conjunct edges cuts,
// and one at and past each cap, the members of an allOf's entries summed. A
// cut leaves no kind known; a cap is no cut, so the kinds stay whole, but past
// it the summary an election compares admits every kind and the read is no
// longer within the caps.
func TestAdmission_ACapStopsTheElectionsSummaryAlone(t *testing.T) {
	t.Parallel()
	serviceContext := servicetest.Context(t, openapitest.ComponentSpecVer("3.1.0", capsSpec()), service.Policy{})
	for name, want := range map[string]struct {
		cut, within bool
		kinds       kindSet
	}{
		"Str": {within: true, kinds: kindString}, "Cyc": {cut: true, kinds: anyInstance},
		"AtReads": {within: true, kinds: kindString}, "PastReads": {kinds: kindString},
		"AtMembers": {within: true, kinds: kindString}, "PastMembers": {kinds: kindString},
		"AtMembersSummed": {within: true, kinds: kindString}, "PastMembersSummed": {kinds: kindString},
	} {
		admission := admissionOf(serviceContext.Ctx, heldAt(serviceContext.Ctx, jsontext.Pointer("/components/schemas/"+name)))
		assert.Equal(t, want.cut, admission.cut, "%s: cut", name)
		assert.Equal(t, want.within, admission.within(), "%s: within the caps", name)
		assert.Equal(t, want.kinds, admission.kinds, "%s: the kinds it lowers to", name)
		assert.Equal(t, want.within, admission.admitted.kinds != kindAny, "%s: the summary an election compares", name)
	}
}

// TestDeadBranch_ReadsABranchPastTheCapsByItsKinds pins deadBranch over a
// branch of strings beside an object body, read within and past the caps: its
// kinds decide it dead at either size, and beside a body that may admit null
// one past them lives, since its null is not read.
func TestDeadBranch_ReadsABranchPastTheCapsByItsKinds(t *testing.T) {
	t.Parallel()
	serviceContext := servicetest.Context(t, openapitest.ComponentSpecVer("3.1.0", capsSpec()), service.Policy{})
	for name, dead := range map[string]bool{
		"ObjectWithin": true, "ObjectPast": true, "NullableWithin": true, "NullablePast": false, "NullableMembersPast": false,
	} {
		st := heldAt(serviceContext.Ctx, jsontext.Pointer("/components/schemas/"+name))
		assert.Equal(t, []bool{dead}, deadBranches(serviceContext.Ctx, st.Schema, st.Schema), name)
	}
}

// capsSpec declares the schemas the tests above read: Str, a cycle Cyc, a
// chain AtReads taking in maxAdmissionReads schemas and PastReads one more,
// enums at and past maxAdmissionMembers, alone and split over two allOf
// entries, and object bodies, nullable or not, beside a branch over a string
// within and past the caps.
func capsSpec() string {
	// AtReads takes in itself, the aliases and Str: maxAdmissionReads schemas.
	chain, prev := "", "Str"
	for i := range maxAdmissionReads - 2 {
		chain += fmt.Sprintf("    A%d: {$ref: '#/components/schemas/%s'}\n", i, prev)
		prev = fmt.Sprintf("A%d", i)
	}
	enum := func(n int) string {
		members := make([]string, n)
		for i := range members {
			members[i] = fmt.Sprintf("v%d", i)
		}
		return "{enum: [" + strings.Join(members, ", ") + "]}"
	}
	half := maxAdmissionMembers / 2
	spec := "    Str: {type: string}\n    Cyc: {allOf: [{$ref: '#/components/schemas/Cyc'}]}\n" + chain +
		"    AtReads: {$ref: '#/components/schemas/" + prev + "'}\n    PastReads: {$ref: '#/components/schemas/AtReads'}\n" +
		"    AtMembers: " + enum(maxAdmissionMembers) + "\n    PastMembers: " + enum(maxAdmissionMembers+1) + "\n" +
		"    AtMembersSummed: {allOf: [" + enum(half) + ", " + enum(half) + "]}\n" +
		"    PastMembersSummed: {allOf: [" + enum(half) + ", " + enum(half+1) + "]}\n"
	for _, b := range [][2]string{{"Within", "Str"}, {"Past", "AtReads"}, {"MembersPast", "PastMembers"}} {
		ref := "anyOf: [{$ref: '#/components/schemas/" + b[1] + "'}]}\n"
		spec += "    Object" + b[0] + ": {type: object, " + ref + "    Nullable" + b[0] + ": {type: [object, 'null'], " + ref
	}
	return spec
}
