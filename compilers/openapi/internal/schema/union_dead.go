// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// deadBranchesOf keys what deadBranches read of one body and union.
type deadBranchesOf struct {
	body, holder *statement.Schema
}

// deadBranches marks, by index, each branch of the union holder writes that no
// instance of body satisfies (deadBranch), read once a run, as every reader of
// the position asks. Where body's own conjuncts share no kind, it admits no
// object for a branch to share, which the warning
// docs/compilers/openapi.md#allof-disjoint-conjuncts gives says of the schema
// whole, so none is marked. That reading leaves null out: the nulls walk would
// classify body's union again.
func deadBranches(c lowering.Ctx, body, holder *statement.Schema) []bool {
	return lowering.Remembered(c, deadBranchesOf{body, holder}, func() []bool {
		branches, _, _ := unionBranches(holder)
		out := make([]bool, len(branches))
		if conjunctsShareNothing(c, body) {
			return out
		}
		for i, b := range branches {
			st := c.Statements().Sub(b)
			out[i] = !nullBranch(c, st) && deadBranch(c, body, st)
		}
		return out
	})
}

// deadBranch reports whether no instance satisfies both body, which admits
// objects alone, null aside, as a union distributing it requires, and the
// union branch b: the kinds b lowers to (lowersTo) hold no object, and b
// admits no null or body keeps it out. Kinds the kind-inference policy read,
// on either side, and a read cut short or past the admission's caps decide
// nothing, so such a branch lives
// (docs/compilers/openapi.md#union-beside-structure).
func deadBranch(c lowering.Ctx, body *statement.Schema, b *statement.Statement) bool {
	if r := admissionOf(c, b); r.reads > maxAdmissionReads || r.members > maxAdmissionMembers {
		return false
	}
	kinds, inferred := lowersTo(c, b)
	if inferred || kinds&kindObject != 0 || bodyKindInferred(c, body) {
		return false
	}
	if bodyForbidsNull(c, body) {
		return true
	}
	// b's read is whole and within the caps: the null reading of b, which may
	// classify a union it reaches and so ask this again, reaches no cycle
	// through body and nests at most maxAdmissionReads deep.
	n := newNulls(c)
	return !n.conjunct(b, nullSite{}).admits(c)
}

// bodyForbidsNull reports whether body keeps null out under the reading c's
// policy selects, by its own type: the union beside it never forbids null
// (unionNullReading), so none of its branches lets one in. Under the
// nullable-inference policy a `nullable: true` on body, or on an entry of an
// allOf it writes, may widen it, so a body writing either forbids nothing
// here, the answer that claims the least; its union is not read again.
func bodyForbidsNull(c lowering.Ctx, body *statement.Schema) bool {
	n := newNulls(c)
	if !c.NullableInference.Honours() {
		return n.declaredTypeVerdict(body) == nullForbidden
	}
	return typeNullVerdict(body) == nullForbidden && !nullableTrue(body) && len(allOfIn(body)) == 0
}

// deadBranchAt is one branch of u that dead marks, with where it is written.
type deadBranchAt struct {
	st *statement.Statement
	at jsontext.Pointer
}

// deadOf returns the branches of u that dead marks, in branch order.
func deadOf(c lowering.Ctx, u unionAt, dead []bool) []deadBranchAt {
	branches, key, _ := unionBranches(u.holder)
	var out []deadBranchAt
	for i, b := range branches {
		if dead[i] {
			out = append(out, deadBranchAt{st: c.Statements().Sub(b), at: u.at + ids.Ptr(key, strconv.Itoa(i))})
		}
	}
	return out
}

// keepDeadBranches keeps each branch of u, a union distributed over body,
// written at pointer, that no instance of body satisfies (u.dead) verbatim
// under p, the Union's Unmodeled, keyed by its path from body, and reports
// them once, at body (diagDeadBranches). Such a branch is no variant and
// lowers nowhere, so this is its one home.
func keepDeadBranches(c lowering.Ctx, p *ir.Unmodeled, body *statement.Schema, u unionAt, pointer jsontext.Pointer,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	all := true
	for _, d := range deadOf(c, u, u.dead) {
		kept, keptDiags := annotation.PreserveNodeInto(p, "openapi:"+pathFrom(pointer, d.at), d.st.Node,
			ir.ReasonDegradedLowering, c.Place().At(d.at))
		diags = append(diags, keptDiags...)
		all = all && kept
	}
	return append(diags, diagDeadBranches(c, body, u, u.dead, pointer, all)...)
}

// diagDeadBranches is the one warning body, written at pointer, draws whose
// union u holds branches no instance of it satisfies, as dead marks them: it
// names each by its path from the body, with the kinds it admits, and says
// whether the union lives on in the others, keeping them where kept says each
// was stored, or the body lowers alone.
func diagDeadBranches(c lowering.Ctx, body *statement.Schema, u unionAt, dead []bool, pointer jsontext.Pointer,
	kept bool,
) []ir.Diagnostic {
	found := deadOf(c, u, dead)
	if len(found) == 0 {
		return nil
	}
	names := make([]string, 0, len(found))
	for _, d := range found {
		kinds, _ := lowersTo(c, d.st)
		word := noValue
		if kinds != 0 {
			word = instanceWords(kinds)
		}
		names = append(names, pathFrom(pointer, d.at)+": "+word)
	}
	_, key, _ := unionBranches(u.holder)
	// The body admits objects alone, and null where it does not keep it out, in
	// which case a dead branch admits no null either (deadBranch).
	opening := fmt.Sprintf("the body beside this %s admits objects alone, null aside, and", key)
	either, neither := "an object or null", "no object and no null"
	if bodyForbidsNull(c, body) {
		opening = fmt.Sprintf("the body beside this %s admits objects alone, and", key)
		either, neither = "an object", "no object"
	}
	if !hasLiveBranch(c, u, dead) {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
			"%s none of its %d branches admits %s (%s), so no instance satisfies this schema, which lowers as its "+
				"body alone", opening, len(found), either, strings.Join(names, ", "))}
	}
	verb, pronoun, rest := "admit", "them", "they are no variants of the union"
	if len(found) == 1 {
		verb, pronoun, rest = "admits", "it", "it is no variant of the union"
	}
	if kept {
		rest += ", kept verbatim under Unmodeled"
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer,
		"%s %d of its branches %s %s (%s), so no instance satisfies %s: %s",
		opening, len(found), verb, neither, strings.Join(names, ", "), pronoun, rest)}
}

// hasLiveBranch reports whether some branch of u is neither dead nor null.
func hasLiveBranch(c lowering.Ctx, u unionAt, dead []bool) bool {
	branches, _, _ := unionBranches(u.holder)
	for i, b := range branches {
		if !dead[i] && !nullBranch(c, c.Statements().Sub(b)) {
			return true
		}
	}
	return false
}

// pathFrom is the path of at below the body written at pointer.
func pathFrom(pointer, at jsontext.Pointer) string {
	return strings.TrimPrefix(string(at), string(pointer)+"/")
}

// keepsDeadUnion reports whether s, read at the position its bindings place it
// at, writes a union beside its object body none of whose branches an
// instance of the body satisfies, which s reports where it is written
// (diagDeadBranches), as distributesUnion reads a distribution there.
func keepsDeadUnion(c lowering.Ctx, s *statement.Schema) bool {
	at, placed := siteOf(s)
	if !placed || !declaresUnion(s) || !hasUnionSiblings(c, s) {
		return false
	}
	in, _ := lowering.InDocument(c, at.Doc)
	return classifyUnionSiblings(in, s, at.Pointer) == unionNoLiveBranch
}
