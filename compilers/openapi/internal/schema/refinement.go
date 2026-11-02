// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// maxRefinedConjuncts bounds the conjuncts one election compares, every pair of
// them: an allOf past it stays a composition, which loses nothing.
const maxRefinedConjuncts = 64

// objectKeywords are the keywords only a Model holds, so a conjunct writing one
// composes as an object whatever else it says.
var objectKeywords = []string{
	"properties", "patternProperties", "additionalProperties", "required", "discriminator",
}

// refinement is an allOf no conjunct can make an object: one use of the conjunct
// that decides it, its principal, rather than a composition
// (docs/compilers/openapi.md#allof-refinement).
type refinement struct {
	comp allOfComposition
	// principal is the branch the position is a use of, nil where the keywords
	// written beside the allOf decide it or nothing does.
	principal *allOfBranch
	// ref is the one $ref branch, principal or not, nil where none is written.
	ref *allOfBranch
	// joint is what the conjuncts admit together.
	joint admitted
	// held is what the position lowers to admits, which decides what a keyword
	// left unread loses.
	held admitted
}

// principalIs reports whether e is the branch the position lowers from.
func (r refinement) principalIs(e allOfBranch) bool {
	return r.principal != nil && r.principal.pointer == e.pointer
}

// conjunct is one conjunct's reading, for the election.
type conjunct struct {
	// branch is nil for the keywords written beside the allOf.
	branch *allOfBranch
	read   admitted
	// says marks an inline conjunct writing what kind of value this is.
	says bool
}

// refinementAt keys a position's decision: where a schema is read from says where
// its branches are, so one schema read from two pointers is two answers.
type refinementAt struct {
	schema  *oas3.Schema
	pointer jsontext.Pointer
}

// decision is what a position's readers share: a refinement, where it is one.
type decision struct {
	r       refinement
	refines bool
}

// refinementOf decides whether s, written at pointer, refines rather than
// composes (docs/compilers/openapi.md#allof-refinement). It reads the source and
// the bindings, never the registry, so a decision turns on no lowering order, and
// each position's many readers share one reading.
func refinementOf(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) (refinement, bool) {
	if !allOfDecides(s) {
		return refinement{}, false
	}
	d := lowering.Remembered(c, refinementAt{s, pointer}, func() decision { return decide(c, s, pointer) })
	return d.r, d.refines
}

// decide reads the decision refinementOf remembers.
func decide(c lowering.Ctx, s *oas3.Schema, pointer jsontext.Pointer) decision {
	if annotation.DeclaresAny(s, objectKeywords) {
		return decision{}
	}
	comp := allOfBranches(s, pointer)
	if !refinableBranches(comp) {
		return decision{}
	}
	w := newAdmission(c)
	conjuncts := w.conjuncts(s, comp)
	joint := conjuncts[0].read
	for _, cj := range conjuncts[1:] {
		joint = joint.and(cj.read)
	}
	ref := refBranchOf(conjuncts)
	says := slices.ContainsFunc(conjuncts, func(cj conjunct) bool { return cj.says })
	if disjoint(joint, conjuncts) || (joint.kinds&kindObject != 0 && (ref != nil || says)) {
		return decision{}
	}
	principal := w.elect(conjuncts, ref)
	r := refinement{comp: comp, principal: principal.branch, joint: joint, held: principal.read}
	if ref != nil {
		r.ref = ref.branch
	}
	return decision{r: r, refines: !w.incomplete()}
}

// refinableBranches reports whether every branch of comp can be read as a
// conjunct of a refinement. A second $ref stays composing: dropping a reference
// for another would drop a branch the source wrote
// (docs/ir/models.md#composition-entries). A union on a branch stays too, until
// lowering its branches gives it a home (GitHub #661).
func refinableBranches(comp allOfComposition) bool {
	if len(comp.branches) > maxRefinedConjuncts {
		return false
	}
	refs := 0
	for _, e := range comp.branches {
		bs := e.schema.GetSchema()
		if e.capped || annotation.IsFalseSchema(e.schema) || declaresUnion(bs) ||
			declaresDynamicRef(bs) || annotation.DeclaresAny(bs, objectKeywords) {
			return false
		}
		if isRefBranch(e.schema) {
			refs++
		}
	}
	return refs <= 1
}

// conjuncts reads s's own keywords, then each branch of comp but a `true`, which
// says nothing. A wrapper's own keywords are read alone: its entries are
// conjuncts of their own.
func (w *admission) conjuncts(s *oas3.Schema, comp allOfComposition) []conjunct {
	out := make([]conjunct, 0, 1+len(comp.branches))
	out = append(out, conjunct{read: w.written(s), says: saysKind(s)})
	for i := range comp.branches {
		e := &comp.branches[i]
		bs := e.schema.GetSchema()
		switch {
		case bs == nil:
		case isRefBranch(e.schema):
			out = append(out, conjunct{branch: e, read: w.whole(e.schema)})
		default:
			out = append(out, conjunct{branch: e, read: w.written(bs), says: saysKind(bs)})
		}
	}
	return out
}

// saysKind reports whether s writes what kind of value it admits.
func saysKind(s *oas3.Schema) bool {
	return writesType(s) || enumWritten(s) || s.GetConst() != nil
}

// refBranchOf returns the conjunct holding the one $ref branch, if any.
func refBranchOf(conjuncts []conjunct) *conjunct {
	for i := range conjuncts {
		if b := conjuncts[i].branch; b != nil && isRefBranch(b.schema) {
			return &conjuncts[i]
		}
	}
	return nil
}

// disjoint reports whether the conjuncts share no value. A null they all admit
// is no value a type is left to describe, unless a conjunct admits nothing else.
// Such an allOf stays a composition, reported only where an inline `type` says
// so (GitHub #1215).
func disjoint(joint admitted, conjuncts []conjunct) bool {
	if !joint.reachable() {
		return true
	}
	return joint.kinds == kindNull &&
		!slices.ContainsFunc(conjuncts, func(cj conjunct) bool { return cj.read.kinds == kindNull })
}

// elect picks the conjunct the position lowers from, a function of the whole set
// but for a tie, which goes to the first and leaves the other kept and reported:
// the inline value set within every other, since it fixes the values and what it
// narrows cannot; else the $ref, since replacing it drops what its target says;
// else, of those naming a kind, the conjunct within the most others, the keywords
// beside the allOf first. A `format` or `items` naming no kind only qualifies one,
// so it never decides, and where nothing names a kind no branch does.
func (w *admission) elect(conjuncts []conjunct, ref *conjunct) *conjunct {
	for i := range conjuncts {
		if narrowsAll(conjuncts, i, ref) {
			return &conjuncts[i]
		}
	}
	if ref != nil {
		return ref
	}
	best, most := 0, 0
	for i := range conjuncts {
		if !conjuncts[i].says {
			continue
		}
		if n := w.conesOver(conjuncts, i); n > most {
			best, most = i, n
		}
	}
	return &conjuncts[best]
}

// narrowsAll reports whether conjunct i is a value set within every other
// conjunct and more than a restatement of what a $ref fixes, which a $ref's own is
// not. Only a branch can be one: a value set beside the allOf is no refinement.
func narrowsAll(conjuncts []conjunct, i int, ref *conjunct) bool {
	cj := conjuncts[i]
	if !cj.read.fixed || (ref != nil && ref.read.valuesWithin(cj.read)) {
		return false
	}
	return !slices.ContainsFunc(conjuncts, func(o conjunct) bool {
		return !cj.read.kindsWithin(o.read) || !cj.read.valuesWithin(o.read)
	})
}

// conesOver counts the conjuncts conjunct i lies within.
func (w *admission) conesOver(conjuncts []conjunct, i int) int {
	n := 0
	for j := range conjuncts {
		if w.within(conjuncts[i].read, conjuncts[j].read) {
			n++
		}
	}
	return n
}

// lowerPrincipal lowers the branch r uses, at the pointer it is written at, so
// nothing is minted and each node stands at a pointer naming its schema. A $ref
// goes through Ref, as fillRefBranch's does, keeping the alias what is written
// beside it always has. An inline branch that reduces to a shared node leaves
// what it writes to the position, as a voice. Either takes a frame of its own, so
// a refinement counts two against maxSchemaDepth, a limit no real schema nears. A
// $ref the principal beat is still resolved: nothing else may lower what it names.
func lowerPrincipal(c lowering.Ctx, ts *compile.Types, depth int, r refinement) (ir.TypeID, []ir.Diagnostic) {
	e := r.principal
	if isRefBranch(e.schema) {
		ref, diags := Ref(c, ts, depth, e.schema, e.pointer)
		return ref.Target, diags
	}
	if r.ref != nil {
		resolveSchemaRef(c, ts, r.ref.schema)
	}
	ref, diags := CarriedRef(c, ts, depth, e.schema, e.pointer)
	return ref.Target, diags
}

// refinedShape describes what a refinement lowers to, for the diagnostic
// recordUnhomedAt reports.
const refinedShape = "a use of one of its allOf conjuncts"

// outcomeKeeps is what a position that lowered to a reference keeps verbatim: the
// census keywords it wrote, described by shape, and the allOf branches its
// lowering left unread.
type outcomeKeeps struct {
	keywords []string
	shape    string
	comp     allOfComposition
	branches []keptBranch
}

// keptBranch is one allOf branch a refinement keeps whole, the keywords nothing
// consumed, none for a $ref the position lost, and whether the conjuncts' disagreeing
// is why.
type keptBranch struct {
	entry   allOfBranch
	residue []string
	split   bool
}

// holdsAny reports whether k keeps anything.
func (k outcomeKeeps) holdsAny() bool { return len(k.keywords) > 0 || len(k.branches) > 0 }

// keepsAtOutcome returns what the position s is written at keeps, having lowered
// to target: a refinement's census and the branches it left, or the census of
// any other outcome lowering to a reference (referenceOutcomeKeywords).
func keepsAtOutcome(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, target ir.TypeRef, pointer jsontext.Pointer) outcomeKeeps {
	r, refined := refinementOf(c, s, pointer)
	if !refined {
		keywords, shape := referenceOutcomeKeywords(c, s, pointer)
		return outcomeKeeps{keywords: keywords, shape: shape}
	}
	k := outcomeKeeps{shape: refinedShape, comp: r.comp}
	if r.principal != nil {
		k.keywords = refinedUnhomed(c, r, s)
	}
	bounds := boundsFold(c, ts, target, pointer)
	voices := voicesAmong(c, s)
	w := newAdmission(c)
	for i, e := range r.comp.branches {
		// A principal keeps what it writes where it lowered: a $ref on its alias, an
		// inline branch on the node it owns, a type array's census included.
		if r.principalIs(e) && (isRefBranch(e.schema) || LoweredToOwnNode(c, ts, e.pointer, target)) {
			continue
		}
		bs := e.schema.GetSchema()
		switch {
		case isRefBranch(e.schema):
			k.branches = append(k.branches, keptBranch{entry: e})
		case bs != nil:
			residue := unmergedBranchKeys(bs, func(key string) bool {
				return (key == "allOf" && e.wrapper) || (voices[i] && folds(c, bs, key, bounds)) || w.holds(r.held, bs, key)
			})
			if len(residue) > 0 {
				k.branches = append(k.branches, keptBranch{entry: e, residue: residue, split: !voices[i] && disagreed(bs, residue)})
			}
		}
	}
	return k
}

// boundsFold reports whether the position has a field for the value bounds its
// voices write: a carrier and an alias do, and a node it owns says for itself.
func boundsFold(c lowering.Ctx, ts *compile.Types, target ir.TypeRef, pointer jsontext.Pointer) bool {
	if !LoweredToOwnNode(c, ts, pointer, target) {
		return true
	}
	td, ok := ts.Node(target.Target)
	return ok && constraintsHome(td)
}

// refinedUnhomed returns the census keywords s writes beside an allOf that
// lowered to its principal, but for those the principal already says, where a
// $ref site reads no target and keeps them all (GitHub #1067).
func refinedUnhomed(c lowering.Ctx, r refinement, s *oas3.Schema) []string {
	w := newAdmission(c)
	said := slices.DeleteFunc(slices.Clone(censusKeywords), func(key string) bool { return !w.holds(r.held, s, key) })
	return unhomedKeywords(s, nil, said)
}

// recordOn keeps k on the node owner names.
func (k outcomeKeeps) recordOn(c lowering.Ctx, ts *compile.Types, owner ir.TypeID, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	td, ok, diags := registeredNode(c, ts, owner, pointer)
	if !ok {
		return diags
	}
	return append(diags, k.record(c, &td.Common().Unmodeled, s, pointer)...)
}

// record keeps k on p and reports it. A branch the IR cannot hold is reported by
// the keeper, and nothing here claims it was kept.
func (k outcomeKeeps) record(c lowering.Ctx, p *ir.Unmodeled, s *oas3.Schema, pointer jsontext.Pointer) []ir.Diagnostic {
	diags := recordUnhomedAt(c, p, s, k.keywords, k.shape, pointer)
	for _, b := range k.branches {
		kept, keptDiags := annotation.PreserveNodeInto(p, k.comp.branchKey(b.entry.pointer),
			b.entry.schema.GetSchema().GetRootNode(), ir.ReasonDegradedLowering, c.ProvenanceAt(b.entry.pointer))
		diags = append(diags, keptDiags...)
		if kept {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s", keptWhy(b)))
		}
	}
	return diags
}

// keptWhy words why a refinement keeps a branch.
func keptWhy(b keptBranch) string {
	switch {
	case len(b.residue) == 0:
		return "the value set of this allOf narrows this $ref, and one node holds one value set, so the $ref is " +
			"kept verbatim under Unmodeled; what its target says of the position still reaches it"
	case b.split:
		return disagreedWhy(b.residue)
	default:
		return "this allOf lowered to a use of one of its conjuncts, which has no home for what this inline branch " +
			"adds; the branch (" + strings.Join(b.residue, ", ") + ") is kept verbatim under Unmodeled"
	}
}

// recordPrincipalResidue keeps what binds a use on a node r's principal owns,
// where no carrier reads it: lowered with no home of its own, the branch keeps it
// as any declaration owning a node does.
func recordPrincipalResidue(c lowering.Ctx, ts *compile.Types, s *oas3.Schema, pointer jsontext.Pointer, home annotation.Home) []ir.Diagnostic {
	r, refined := refinementOf(c, s, pointer)
	if !refined || r.principal == nil {
		return nil
	}
	return recordDeclarationResidue(c, ts, r.principal.schema.GetSchema(), r.principal.pointer, home)
}
