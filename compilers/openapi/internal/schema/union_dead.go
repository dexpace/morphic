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
// the position asks. Where body's own conjuncts share no kind, the entry
// holder is aside, it admits no object for a branch to share, which the
// warning docs/compilers/openapi.md#allof-disjoint-conjuncts gives says of the
// schema whole, so none is marked. That reading leaves null out: the nulls
// walk would classify body's union again.
func deadBranches(c lowering.Ctx, body, holder *statement.Schema) []bool {
	return lowering.Remembered(c, deadBranchesOf{body, holder}, func() []bool {
		branches, _, _ := unionBranches(holder)
		out := make([]bool, len(branches))
		var aside *statement.Schema
		if holder != body {
			aside = holder
		}
		if conjunctsShareNothing(c, body, aside) {
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
// under p, the Union's Unmodeled (unionBranchKey), and reports them once, at
// body (diagDeadBranches). Such a branch is no variant and lowers nowhere, so
// this is its one home.
func keepDeadBranches(c lowering.Ctx, p *ir.Unmodeled, body *statement.Schema, u unionAt, pointer jsontext.Pointer,
) []ir.Diagnostic {
	var diags []ir.Diagnostic
	all := true
	for _, d := range deadOf(c, u, u.dead) {
		kept, keptDiags := annotation.PreserveNodeInto(p, unionBranchKey(d.at), d.st.Node,
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
	r := deadReport{listed: strings.Join(names, ", "), dead: len(found)}
	r.branches, r.key, _ = unionBranches(u.holder)
	if u.branch {
		r.on = pathFrom(pointer, u.at)
	}
	if hasLiveBranch(c, u, dead) {
		return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "%s",
			r.some(bodyForbidsNull(c, body), kept))}
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, pointer, "%s",
		r.none(c, body))}
}

// deadReport is what diagDeadBranches says of a union: its keyword and
// branches, the dead ones listed, and on, the path from the body of the allOf
// entry it is written on, empty for a union beside the body.
type deadReport struct {
	branches []statement.Sub
	key, on  string
	listed   string
	dead     int
}

// subject opens a report of a union beside its body, which admits objects
// alone, and null where it does not keep it out, in which case a dead branch
// admits no null either (deadBranch): it returns the opening and how to say
// that a branch admits some of what the body admits, and none of it.
func (r deadReport) subject(forbids bool) (opening, either, neither string) {
	if forbids {
		return "the body beside this " + r.key + " admits objects alone, and", "an object", "no object"
	}
	return "the body beside this " + r.key + " admits objects alone, null aside, and", "an object or null",
		"no object and no null"
}

// some is the report of dead branches beside live ones: they are no variants,
// kept verbatim where kept says so. A union on an entry is judged against the
// rest of the allOf, which may admit more kinds than an object, none a
// distributed branch admits.
func (r deadReport) some(forbids, kept bool) string {
	verb, pronoun, rest := "admit", "them", "they are no variants of the union"
	if r.dead == 1 {
		verb, pronoun, rest = "admits", "it", "it is no variant of the union"
	}
	if kept {
		rest += ", kept verbatim under Unmodeled"
	}
	if r.on != "" {
		return fmt.Sprintf("%d of the %d branches of the %s on %s %s no value the rest of the allOf admits (%s), "+
			"so no instance satisfies %s: %s", r.dead, len(r.branches), r.key, r.on, verb, r.listed, pronoun, rest)
	}
	opening, _, neither := r.subject(forbids)
	return fmt.Sprintf("%s %d of its branches %s %s (%s), so no instance satisfies %s: %s",
		opening, r.dead, verb, neither, r.listed, pronoun, rest)
}

// none is the report of a union no branch of which lives, each but its null
// ones dead: the schema lowers as its body alone, and admits null alone where
// the body admits null and the union takes it, by its one null branch or by
// any of them where it is an anyOf.
func (r deadReport) none(c lowering.Ctx, body *statement.Schema) string {
	nulls := len(r.branches) - r.dead
	forbids := bodyForbidsNull(c, body)
	if r.on != "" {
		// A body writing its union on an entry writes an allOf, which
		// bodyForbidsNull leaves unread; the nulls walk reads it whole.
		n := newNulls(c)
		forbids = !n.schema(body, nullSite{}).admits(c)
	}
	but, none := "", "no instance"
	if nulls > 0 && !forbids {
		but = " but its null ones"
		if nulls == 1 {
			but = " but a null one"
		}
		if nulls == 1 || r.key == "anyOf" {
			none = "no instance but null"
		}
	}
	if r.on != "" {
		return fmt.Sprintf("none of the %d branches of the %s on %s%s admits a value the rest of the allOf admits "+
			"(%s), so %s satisfies this schema, which lowers as the rest of the allOf",
			len(r.branches), r.key, r.on, but, r.listed, none)
	}
	opening, either, _ := r.subject(forbids)
	return fmt.Sprintf("%s none of its %d branches%s admits %s (%s), so %s satisfies this schema, which lowers as "+
		"its body alone", opening, len(r.branches), but, either, r.listed, none)
}

// diagDeadEntryUnion is the one warning s, written at pointer, draws where u,
// the union on an entry of its allOf, keeps no branch beside the rest of it
// (unionNoLiveBranch) and the report of conjuncts sharing no value
// (docs/compilers/openapi.md#allof-disjoint-conjuncts) is silent: that report
// reads a union as the nulls walk does, admitting a null the rest admits,
// where a union no branch of which admits null refuses it.
func diagDeadEntryUnion(c lowering.Ctx, s *statement.Schema, u unionAt, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(disjointConjuncts(c, s, allOfBranches(c, s, pointer))) > 0 {
		return nil
	}
	return diagDeadBranches(c, s, u, deadBranches(c, s, u.holder), pointer, false)
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

// unionBranchKey is the Unmodeled key a branch of a distributed union, written
// at at, is kept under: `openapi:<keyword>/<index>`, the last two tokens of at,
// its provenance saying where it is written, beside the body or on an allOf
// entry, as an entry's own keywords are kept. A position distributes one union
// (findBranchUnion), so no two branches it keeps share a key.
func unionBranchKey(at jsontext.Pointer) string {
	index := strings.LastIndexByte(string(at), '/')
	keyword := strings.LastIndexByte(string(at[:index]), '/')
	return "openapi:" + string(at[keyword+1:])
}
