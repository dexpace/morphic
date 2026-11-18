// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"cmp"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// outcomeKeeps is what a position that lowered to a reference keeps verbatim: the
// census keywords it wrote, described by shape where no refinement decided it,
// the value bounds of those a number carried as text holds none of (text), the
// $ref branches of its allOf its lowering left unread, what its inline branches
// keep (entryKeeps), the null branches a collapsed union lifts that say more
// than their null (nullBranchKeeps), and the union keywords beside the allOf it
// keeps as validation logic, their branches declaring no shape (keptUnion).
type outcomeKeeps struct {
	keywords   []string
	text       []string
	shape      string
	comp       allOfComposition
	branches   []keptBranch
	entries    []keptEntry
	nulls      []keptNullBranch
	validation []string
	// refined marks a refinement deciding the position, which electing
	// principal, nil for none, describes it (refinedShape).
	refined   bool
	principal *allOfBranch
}

// keptBranch is one $ref branch of an allOf a refinement keeps whole, the
// position having lost it, and how it stood in the election (voteOf).
type keptBranch struct {
	entry allOfBranch
	vote  refVote
}

// holdsAny reports whether k keeps anything.
func (k outcomeKeeps) holdsAny() bool {
	return len(k.keywords) > 0 || len(k.text) > 0 || len(k.branches) > 0 || len(k.nulls) > 0 || len(k.validation) > 0 ||
		slices.ContainsFunc(k.entries, keptEntry.keeps)
}

// keepsAtOutcome returns what the position s is written at keeps, having lowered
// to target: a refinement's census and the branches it left, or the census of
// any other outcome lowering to a reference (referenceOutcomeKeywords).
func keepsAtOutcome(c lowering.Ctx, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) outcomeKeeps {
	r, refined := refinementOf(c, s, pointer)
	if !refined {
		keywords, shape := referenceOutcomeKeywords(c, s, pointer)
		k := outcomeKeeps{keywords: keywords, shape: shape}
		if shape == collapsedShape {
			k.nulls = nullBranchKeeps(c, s, pointer)
		}
		return k
	}
	k := outcomeKeeps{comp: r.comp, refined: true, principal: r.principal}
	typed := refinedValueType(c, r, s, pointer)
	homes := homesAt(c, s, target, pointer)
	if r.principal != nil {
		degraded, validation := keptUnion(c, r, s)
		k.text, k.keywords = textBoundsOf(refinedUnhomed(c, r, s, typed, homes), homes.numberText)
		k.keywords, k.validation = append(k.keywords, degraded...), validation
	}
	collapsed := collapsedPrincipal(c, r)
	if collapsed {
		k.nulls = nullBranchKeeps(c, r.principal.schema.Schema, r.principal.pointer)
	}
	voices := voicingOf(c, s)
	w := newAdmissionReader(c)
	standing := standsAtPosition(c, r, target, pointer)
	residues := make([][]string, len(r.comp.branches))
	for i, e := range r.comp.branches {
		// A $ref principal keeps what is written beside it on its alias. A branch
		// written as the principal is says nothing it does not. An inline
		// principal standing at the position had its census run on the node,
		// which keeps the rest as any conjunct's.
		if r.principalIs(e) && e.schema.Ref != nil || principalTwin(&w, r, e) {
			continue
		}
		censused := standing && r.principalIs(e)
		switch {
		case e.schema.Ref != nil:
			k.branches = append(k.branches, keptBranch{entry: e, vote: r.votes[i]})
		case stated(e.schema.Schema):
			read := residueRead(c, &w, r, typed, e, voices.speaks[i], censused, collapsed && r.principalIs(e), homes)
			residues[i] = unmergedBranchKeys(e.schema.Schema, read)
		}
	}
	k.entries = entryKeeps(c, s, r.comp, func(i int) []string { return residues[i] }, voices, isAdditionalProperties)
	for i := range k.entries {
		k.entries[i].numberText = homes.numberText
	}
	return k
}

// residueRead reports whether a key the entry e of r writes is read where the
// position lowered, leaving no residue: a wrapper's allOf, what folds where e
// speaks, what the principal says (said), and a key of censusDomain the
// principal standing at the position, e where censused says so, reads or keeps
// on its node (keepUnread). A value set's union and content vocabulary are
// kept, as its node holds neither; any other principal's union is its node's,
// or the reference's where it collapsed (collapsed, collapsedPrincipal), as is
// the content vocabulary of one whose types take it (holdsContent).
func residueRead(c lowering.Ctx, w *admissionReader, r refinement, typed setType, e allOfBranch, speaks, censused, collapsed bool,
	homes boundHomes,
) func(string) bool {
	bs := e.schema.Schema
	_, set := valueSetPrincipal(r)
	return func(key string) bool {
		switch {
		case key == "allOf" && e.wrapper, speaks && folds(c, bs, key, homes), collapsed && unionKeyword(key):
			return true
		case censused && unionKeyword(key):
			return !set
		case censused && slices.Contains(contentKeywords, key):
			return !set && takesContent(declaredInstanceTypes(bs))
		case censused:
			return keyBit(key) != 0 || said(w, r, typed, bs, key)
		default:
			return said(w, r, typed, bs, key)
		}
	}
}

// collapsedPrincipal reports whether r's principal is an inline union that
// lowered as the nullable reference to its one non-null branch, the outcome
// the same union written alone at a position has (referenceOutcomeKeywords):
// the reference says the union, its target the branch and its Nullable the
// null branches, of which one saying more is kept apart (nullBranchKeeps).
func collapsedPrincipal(c lowering.Ctx, r refinement) bool {
	if r.principal == nil || r.principal.schema.Ref != nil || !declaresUnion(r.principal.schema.Schema) {
		return false
	}
	_, shape := referenceOutcomeKeywords(c, r.principal.schema.Schema, r.principal.pointer)
	return shape == collapsedShape
}

// unionKeyword reports whether keyword is oneOf or anyOf.
func unionKeyword(keyword string) bool { return keyword == "oneOf" || keyword == "anyOf" }

// isAdditionalProperties reports whether keyword is additionalProperties: the
// closure no use of a conjunct types, which reads the keywords beside it.
func isAdditionalProperties(keyword string) bool { return keyword == "additionalProperties" }

// principalTwin reports whether e, a branch of r other than its principal, is
// written as the principal is (sameText): one schema written twice,
// whose first copy the election takes for both.
func principalTwin(w *admissionReader, r refinement, e allOfBranch) bool {
	return r.principal != nil && !r.principalIs(e) && sameText(w.c, r.principal.schema, e.schema)
}

// textBoundsOf splits unhomed, the keywords a position keeps, into the value
// bounds a node carrying a number as text holds none of, where numberText says
// the node carries one, which it keeps as it keeps them written beside its
// own keywords (keepTextBounds), and the rest.
func textBoundsOf(unhomed []string, numberText bool) (text, rest []string) {
	if !numberText {
		return nil, unhomed
	}
	for _, key := range unhomed {
		if slices.Contains(valueConstraintKeywords, key) {
			text = append(text, key)
		} else {
			rest = append(rest, key)
		}
	}
	return text, rest
}

// refinedUnhomed returns the census keywords s writes beside an allOf that
// lowered to its principal, but for those the principal already says and the
// bounds the node the position lowered to reads, which homes says
// (boundHomes.reads), where a $ref site reads no target and keeps them all
// (GitHub #1067). typed is the type of the value set the principal lowered to,
// if any (refinedValueType). A value set keeps them on its own node, in its
// build (lowerPrincipalValueSet), so the outcome keeps none.
func refinedUnhomed(c lowering.Ctx, r refinement, s *statement.Schema, typed setType, homes boundHomes) []string {
	if _, atPosition := valueSetPrincipal(r); atPosition {
		return nil
	}
	w := newAdmissionReader(c)
	var held keySet
	for _, key := range nodeKeywords {
		if said(&w, r, typed, s, key) {
			held |= keyBit(key)
		}
	}
	return unreadKeys(s, homes.reads(s)|readElsewhere(s)|held)
}

// keptUnion returns the union keywords s writes beside an allOf that lowered to
// its principal r, a keyword beside a refinement, that the position keeps: none
// the principal says (unionHeld), as validation logic those whose branches
// declare no shape, as the same union beside any body is, and every other one
// degraded (docs/compilers/openapi.md#allof-refinement).
func keptUnion(c lowering.Ctx, r refinement, s *statement.Schema) (degraded, validation []string) {
	if !declaresUnion(s) {
		return nil, nil
	}
	shapeless := !unionBranchesDeclareShape(c, s)
	w := newAdmissionReader(c)
	for _, key := range []string{"oneOf", "anyOf"} {
		switch {
		case !hasKey(s, key) || unionHeld(&w, r, s, key):
		case shapeless:
			validation = append(validation, key)
		default:
			degraded = append(degraded, key)
		}
	}
	return degraded, validation
}

// said reports whether the principal of r already says what s's keyword key
// does: what it admits holds it, a union one of whose branches it restates
// (unionHeld), or it is a format the value set it lowered to, typed, holds in
// its ValueType, that format's primitive or a narrower one, or a redaction. A
// type that ValueType holds, its values hold too.
func said(w *admissionReader, r refinement, typed setType, s *statement.Schema, key string) bool {
	if w.holds(r.held, s, key) || unionHeld(w, r, s, key) || key == "type" && nullIdiom(w.c, s) {
		return true
	}
	return typed.ok && key == "format" && (annotation.DeclaresRedaction(s) || valueTypeHoldsFormat(typed.prim, s))
}

// unionHeld reports whether every value r holds satisfies s's oneOf or anyOf,
// the keyword key names: a branch says nothing r's principal does not
// (admissionReader.restates), and for a oneOf one alone, sharing no kind with any
// other, so that exactly one admits each of its values. A branch is compared
// keyword by keyword, as what it admits leaves its bounds and annotations unread,
// and one writing what no reading compares says more. A read cut short says no.
func unionHeld(w *admissionReader, r refinement, s *statement.Schema, key string) bool {
	var branches []statement.Sub
	switch key {
	case "oneOf":
		branches = s.ChoiceKeywords().OneOf
	case "anyOf":
		branches = s.ChoiceKeywords().AnyOf
	default:
		return false
	}
	principal := w.c.Statements().Read(nil)
	if r.principal != nil {
		principal = r.principal.schema
	}
	within, overlaps := 0, 0
	for _, b := range branches {
		branch := w.c.Statements().Sub(b)
		switch {
		case w.restates(r.held, principal, branch):
			within++
		case r.held.kinds&lowered(w, branch).kinds != 0:
			overlaps++
		}
	}
	if w.incomplete() || within == 0 {
		return false
	}
	return key == "anyOf" || (within == 1 && overlaps == 0)
}

// record keeps k on p and reports it, standing being the node the position
// owns, nil where it owns none (refinedShape). A branch the IR cannot hold is
// reported by the keeper, and nothing here claims it was kept.
func (k outcomeKeeps) record(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer,
	standing ir.TypeDef,
) []ir.Diagnostic {
	shape := k.shape
	if k.refined {
		shape = refinedShape(c, k.comp, k.principal, standing)
	}
	diags := recordUnhomedAt(c, ts, p, s, k.keywords, shape, pointer)
	diags = append(diags, keepTextBounds(c, p, s, k.text, pointer)...)
	for _, key := range k.validation {
		diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, s, key, pointer, c.Place())...)
	}
	diags = append(diags, recordNullBranches(c, p, k.nulls)...)
	kept, keptDiags := keepRefConjuncts(c, p, k.branches, pointer)
	diags = append(diags, keptDiags...)
	for _, b := range k.branches {
		if kept[b.entry.pointer] {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s", keptWhy(k.comp, b)))
		}
	}
	for _, e := range k.entries {
		diags = append(diags, recordKeptEntry(c, ts, p, k.comp, e, shape, false, func(e keptEntry) ir.Diagnostic {
			return c.DiagAt(ir.SeverityInfo, wholeCode(e), e.entry.pointer, "%s", keptEntryWhy(k.comp, e, k.principal == nil, shape))
		})...)
	}
	dropFoldedIntoWhole(c, p)
	return diags
}

// keptRef is the key a kept $ref conjunct is kept under, and where the entry
// is said to be written.
type keptRef struct {
	key string
	at  jsontext.Pointer
}

// keptRefKeys returns, by its pointer, the key each $ref of branches is kept
// under: openapi:$ref where one is kept, else openapi:$ref/<i>, i its rank by
// reference text, then by the entry as written, so no order of the entries
// decides (docs/compilers/openapi.md#allof-refinement). Entries written alike
// are one entry, at pointer, the position writing them.
func keptRefKeys(branches []keptBranch, pointer jsontext.Pointer) map[jsontext.Pointer]keptRef {
	alike := map[[2]string][]jsontext.Pointer{}
	for _, b := range branches {
		raw, err := annotation.RawFromNode(b.entry.schema.Node)
		if err != nil {
			// An entry that cannot render is alike to no other: its pointer,
			// which no JSON text spells, ranks it.
			raw = ir.RawValue(b.entry.pointer)
		}
		_, text := refSpelling(b.entry.schema)
		alike[[2]string{text, string(raw)}] = append(alike[[2]string{text, string(raw)}], b.entry.pointer)
	}
	written := slices.SortedFunc(maps.Keys(alike), func(a, b [2]string) int {
		return cmp.Or(strings.Compare(a[0], b[0]), strings.Compare(a[1], b[1]))
	})
	out := make(map[jsontext.Pointer]keptRef, len(branches))
	for i, w := range written {
		key := "openapi:$ref"
		if len(written) > 1 {
			key += "/" + strconv.Itoa(i)
		}
		for _, at := range alike[w] {
			out[at] = keptRef{key: key, at: at}
			if len(alike[w]) > 1 {
				out[at] = keptRef{key: key, at: pointer}
			}
		}
	}
	return out
}

// keptEntryWhy words why a refinement keeps an entry whole, undecided
// saying it elected no entry, naming the schema writing its conjuncts as comp's
// subject does and what it lowered to as shape (refinedShape).
func keptEntryWhy(comp allOfComposition, e keptEntry, undecided bool, shape string) string {
	if why := wholeWhy(e); why != "" {
		return why
	}
	of, n := comp.subject(), namesOf(e.entry)
	if undecided {
		return "no conjunct of " + of + " is the one it lowers from, so it lowered by the keywords beside them, " +
			"where those name no kind by the primitive kinds its entries admit together, else to the top type, and " +
			"nothing there homes what " + n.this + " adds; " + n.kept + " (" +
			strings.Join(e.residue, ", ") + ") is kept verbatim under Unmodeled"
	}
	return of + " lowered to " + shape + ", which has no home for what " + n.this + " " +
		"adds; " + n.kept + " (" + strings.Join(e.residue, ", ") + ") is kept verbatim under Unmodeled"
}

// keepRefConjuncts keeps on p each $ref conjunct of branches, one a
// refinement, a composition or a failed read leaves unlowered: the reference
// as written, its text alone, under the key keptRefKeys ranks it by, at the
// entry writing it. What an entry writes beside its $ref is kept apart, as an
// inline entry's keywords are. It reports which it kept, by pointer.
func keepRefConjuncts(c lowering.Ctx, p *ir.Unmodeled, branches []keptBranch, pointer jsontext.Pointer,
) (map[jsontext.Pointer]bool, []ir.Diagnostic) {
	var keys map[jsontext.Pointer]keptRef
	if len(branches) == 1 {
		// One kept $ref is openapi:$ref at its entry, as keptRefKeys ranks it.
		keys = map[jsontext.Pointer]keptRef{branches[0].entry.pointer: {key: "openapi:$ref", at: branches[0].entry.pointer}}
	} else {
		keys = keptRefKeys(branches, pointer)
	}
	kept := make(map[jsontext.Pointer]bool, len(branches))
	var diags []ir.Diagnostic
	for _, b := range branches {
		_, text := refSpelling(b.entry.schema)
		ref := keys[b.entry.pointer]
		ok, keptDiags := annotation.PreserveNodeInto(p, ref.key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: text},
			ir.ReasonDegradedLowering, c.Place().At(ref.at))
		diags = append(diags, keptDiags...)
		kept[b.entry.pointer] = ok
	}
	return kept, diags
}

// keptWhy words why a refinement keeps a $ref branch, naming the schema
// writing its conjuncts as comp's subject does.
func keptWhy(comp allOfComposition, b keptBranch) string {
	of := comp.subject()
	switch b.vote {
	case refInferredApart:
		return "the kind-inference policy reads this $ref's target as kinds leaving out one the rest of " + of +
			" admits, so the $ref is kept verbatim under Unmodeled"
	case refAdmitsEvery:
		return "this $ref's target admits every kind, and what the other conjuncts of " + of + " name, a kind " +
			"or a union, decides first, so the $ref is kept verbatim under Unmodeled"
	case refNarrowed:
		return "another $ref of " + of + " lies within as many of its conjuncts or more, a tie going to the " +
			"least reference text, then to the least text of its entry, and a use names one target, so this " +
			"$ref is kept verbatim under Unmodeled"
	case refOutsideSet:
		return "the conjuncts of " + of + " share no value, and its value set is elected before the rest, " +
			"holding values this $ref's target does not, so the $ref is kept verbatim under Unmodeled"
	case refApart:
		return "the value sets of " + of + " share no member, so no conjunct is the one it lowers from, and the " +
			"$ref is kept verbatim under Unmodeled"
	default:
		return "the value set of " + of + " narrows this $ref, and one node holds one value set, so the $ref is " +
			"kept verbatim under Unmodeled; what its target says of the position still reaches it"
	}
}

// keepConjuncts keeps on p the conjuncts s, written at pointer, writes beside
// its own keywords, which an election passed over: where they are references
// alone, beside them or on its allOf's entries, each as a refinement keeps a
// $ref it passes over (keepRefConjuncts), so `{allOf: [{$ref: T}], K}` keeps
// what `{$ref: T, K}` does; else the $ref written beside them so, and the
// allOf whole. It returns the keywords it kept, `$ref` and `allOf`, in that
// order, and how a report names what p holds of each: an allOf of references
// as its entries' $refs.
func keepConjuncts(c lowering.Ctx, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer,
) (written, named []string, diags []ir.Diagnostic) {
	if refs, ok := refConjuncts(c, s, pointer); ok {
		kept, keptDiags := keepRefConjuncts(c, p, refs, pointer)
		if kept[pointer] {
			written, named = append(written, "$ref"), append(named, "$ref")
		}
		entries := 0
		for _, b := range refs {
			if b.entry.pointer != pointer && kept[b.entry.pointer] {
				entries++
			}
		}
		if entries > 0 {
			as := "the $ref of its allOf"
			if entries > 1 {
				as = "the $refs of its allOf"
			}
			written, named = append(written, "allOf"), append(named, as)
		}
		return written, named, keptDiags
	}
	if s.BesideRef != nil {
		e := allOfBranch{schema: c.Statements().Read(s.BesideRef), pointer: pointer}
		kept, keptDiags := keepRefConjuncts(c, p, []keptBranch{{entry: e}}, pointer)
		diags = append(diags, keptDiags...)
		if kept[pointer] {
			written, named = append(written, "$ref"), append(named, "$ref")
		}
	}
	kept, keptDiags := annotation.PreserveSchemaKeyword(p, s, "allOf", ir.ReasonDegradedLowering, pointer, c.Place())
	diags = append(diags, keptDiags...)
	if kept {
		written, named = append(written, "allOf"), append(named, "allOf")
	}
	return written, named, diags
}

// refConjuncts returns the conjuncts s, written at pointer, writes beside its
// own keywords, each a $ref, and false where one is not.
func refConjuncts(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) ([]keptBranch, bool) {
	branches := allOfBranches(c, s, pointer).branches
	refs := make([]keptBranch, 0, len(branches))
	for _, e := range branches {
		if e.schema.Ref == nil {
			return nil, false
		}
		refs = append(refs, keptBranch{entry: e})
	}
	return refs, len(refs) > 0
}
