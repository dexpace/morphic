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
// unread keywords it wrote, described by shape where no refinement decided it,
// the value bounds of those a number carried as text holds none of (text), the
// $ref entries of its allOf its lowering left unread, what its inline entries
// keep (entryKeeps), the null branches a collapsed union lifts that say more
// than their null (nullBranchKeeps), and the union keywords beside the allOf it
// keeps as validation logic, their branches declaring no shape (keptUnion).
type outcomeKeeps struct {
	keywords   []string
	text       []string
	shape      string
	comp       allOfComposition
	refEntries []wholeRefEntry
	entries    []keptEntry
	nulls      []keptNullBranch
	validation []string
	// refined marks a refinement deciding the position, which electing
	// principal, nil for none, describes it (refinedShape).
	refined   bool
	principal *allOfEntry
}

// wholeRefEntry is one $ref entry of an allOf a refinement keeps whole, the
// position having lost it, and how it stood in the election (voteOf).
type wholeRefEntry struct {
	entry allOfEntry
	vote  refVote
}

// holdsAny reports whether k keeps anything.
func (k outcomeKeeps) holdsAny() bool {
	return len(k.keywords) > 0 || len(k.text) > 0 || len(k.refEntries) > 0 || len(k.nulls) > 0 || len(k.validation) > 0 ||
		slices.ContainsFunc(k.entries, keptEntry.keeps)
}

// keepsAtOutcome returns what the position s is written at keeps, having lowered
// to target: what a refinement keeps unread and the entries it left, or what
// any other outcome lowering to a reference keeps unread (referenceOutcomeKeywords).
func keepsAtOutcome(c lowering.Ctx, s *statement.Schema, target ir.TypeRef, pointer jsontext.Pointer) outcomeKeeps {
	r, refined := refinementOf(c, s, pointer)
	if !refined {
		keywords, shape := referenceOutcomeKeywords(c, s, pointer)
		k := outcomeKeeps{keywords: keywords, shape: shape}
		if shape == collapsedShape {
			k.nulls = nullBranchKeeps(c, s, s, pointer)
		}
		return k
	}
	k := outcomeKeeps{comp: r.comp, refined: true, principal: r.principal}
	typed := refinedValueType(c, r, s, pointer)
	homes := homesAt(c, s, target, pointer)
	standing := standsAtPosition(c, r, target, pointer)
	if r.principal != nil {
		degraded, validation := keptUnion(c, r, s)
		if standing {
			// A union the position writes as its standing principal does is
			// one writing, the principal's (refinedUnread).
			bs := r.principal.schema.Schema
			alike := func(key string) bool { return keyWrittenAlike(s, bs, key) }
			degraded, validation = slices.DeleteFunc(degraded, alike), slices.DeleteFunc(validation, alike)
		}
		k.text, k.keywords = textBoundsOf(refinedUnread(c, r, s, typed, homes, standing), homes.numberText)
		k.keywords, k.validation = append(k.keywords, degraded...), validation
	}
	collapsed := collapsedPrincipal(c, r, s)
	if collapsed {
		k.nulls = nullBranchKeeps(c, s, r.principal.schema.Schema, r.principal.pointer)
	}
	voices := voicingOf(c, s)
	w := newAdmissionReader(c)
	pair := writesUnionPair(s, r.comp)
	residues := make([][]string, len(r.comp.entries))
	for i, e := range r.comp.entries {
		// A $ref principal keeps what is written beside it on its alias. An entry
		// written as the principal is says nothing it does not. An inline
		// principal standing at the position had its unread keywords kept on the node,
		// which keeps the rest as any conjunct's.
		if r.principalIs(e) && e.schema.Ref != nil || principalTwin(&w, r, e) {
			continue
		}
		standsHere := standing && r.principalIs(e)
		switch {
		case e.schema.Ref != nil:
			k.refEntries = append(k.refEntries, wholeRefEntry{entry: e, vote: r.votes[i]})
		case stated(e.schema.Schema):
			// The principal's node keeps what it passes over (preserveUnusedCombinator).
			read := residueRead(c, &w, r, typed, e, voices.speaks[i], standsHere, collapsed && r.principalIs(e), homes,
				pair && !r.principalIs(e))
			residues[i] = unmergedEntryKeys(e.schema.Schema, lessPattern(read, standsHere && patternBeside(s, e.schema.Schema, homes)))
		}
	}
	k.entries = entryKeeps(c, s, r.comp, func(i int) []string { return residues[i] }, voices, isAdditionalProperties, r.principal)
	for i := range k.entries {
		k.entries[i].numberText = homes.numberText
	}
	return k
}

// residueRead reports whether a key the entry e of r writes is read where the
// position lowered, leaving no residue: a wrapper's allOf, what folds where e
// speaks, what the principal says (said) but a union of a pair (pair,
// writesUnionPair), and a key of readDomain the principal standing at the
// position, e where standsHere says so, reads or keeps on its node (keepUnread).
// A value set's union and content vocabulary are kept, as its node holds
// neither; another principal's union is its node's, or the collapsed
// reference's (collapsedPrincipal), and its content vocabulary its own
// reading's (unplacedContent).
func residueRead(c lowering.Ctx, w *admissionReader, r refinement, typed setType, e allOfEntry, speaks, standsHere, collapsed bool,
	homes boundHomes, pair bool,
) func(string) bool {
	bs := e.schema.Schema
	_, set := valueSetPrincipal(r)
	return func(key string) bool {
		switch {
		case key == "allOf" && e.wrapper, speaks && folds(c, bs, key, homes), collapsed && unionKeyword(key):
			return true
		case pair && unionKeyword(key):
			// A union of a pair is kept whatever the principal says, as one
			// schema writing both keeps the one it passes over.
			return false
		case standsHere && unionKeyword(key):
			return !set
		case standsHere && slices.Contains(contentKeywords, key):
			// The principal's own reading keeps what its node holds no
			// Encoding for (lowerPrincipal), so the entry keeps none again.
			return !set
		case standsHere:
			return keyBit(key) != 0 || said(w, r, typed, bs, key)
		default:
			return said(w, r, typed, bs, key)
		}
	}
}

// lowersFrom reports whether e is an entry the position lowers from, principal
// being its inline principal, nil or a $ref for none: that principal, or a
// wrapper it is an entry of at any depth, out of whose allOf it is flattened.
// Each is lowered, the principal as the node and a wrapper as the keywords
// beside the principal's allOf, so what it keeps that another conjunct writes
// otherwise is held against the node's keys and never keeps it whole
// (wholePrincipal).
func lowersFrom(principal *allOfEntry, e allOfEntry) bool {
	if principal == nil || principal.schema.Ref != nil {
		return false
	}
	if principal.pointer == e.pointer {
		return true
	}
	return e.wrapper && strings.HasPrefix(string(principal.pointer), string(e.pointer)+"/")
}

// lessPattern returns read, which keys of an inline principal are read, less
// its pattern where residue says the node holds the position's instead, so the
// principal's is residue, and the two disagree (ConstraintsOf).
func lessPattern(read func(string) bool, residue bool) func(string) bool {
	if !residue {
		return read
	}
	return func(key string) bool { return key != "pattern" && read(key) }
}

// patternBeside reports whether s writes a pattern beside its allOf otherwise
// than bs, the inline principal whose node, holding what homes says, takes a
// string's bounds: one field holds one pattern, the position's.
func patternBeside(s, bs *statement.Schema, homes boundHomes) bool {
	if !hasKey(s, "pattern") || !hasKey(bs, "pattern") || !taken(homes.value, "pattern") {
		return false
	}
	return s.TextKeywords().Pattern != bs.TextKeywords().Pattern
}

// writesUnionPair reports whether s and the inline entries of comp, the allOf
// it writes, write a oneOf and an anyOf between them: two unions competing
// keywords elects between (docs/compilers/openapi.md#competing-keywords), of
// which a refinement folds neither into its principal as said, as one schema
// writing both keeps the one it passes over whatever the other says.
func writesUnionPair(s *statement.Schema, comp allOfComposition) bool {
	oneOf, anyOf := false, false
	note := func(x *statement.Schema) {
		choice := x.ChoiceKeywords()
		oneOf, anyOf = oneOf || len(choice.OneOf) > 0, anyOf || len(choice.AnyOf) > 0
	}
	note(s)
	for _, e := range comp.entries {
		if e.schema.Ref == nil {
			note(e.schema.Schema)
		}
	}
	return oneOf && anyOf
}

// collapsedPrincipal reports whether r's principal is an inline union that
// lowered as the nullable reference to its one non-null branch, the outcome
// the same union written alone at a position has (referenceOutcomeKeywords):
// the reference says the union, its target the branch and its Nullable the
// null branches, of which one saying more is kept apart (nullBranchKeeps). A
// union s elects over the other of a pair does not collapse (pairedPrincipal).
func collapsedPrincipal(c lowering.Ctx, r refinement, s *statement.Schema) bool {
	if r.principal == nil || r.principal.schema.Ref != nil || !declaresUnion(r.principal.schema.Schema) || electsOneOfPair(c, s) {
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

// principalTwin reports whether e, an entry of r other than its principal, is
// written as the principal is (sameText): one schema written twice,
// whose first copy the election takes for both.
func principalTwin(w *admissionReader, r refinement, e allOfEntry) bool {
	return r.principal != nil && !r.principalIs(e) && sameText(w.c, r.principal.schema, e.schema)
}

// textBoundsOf splits unhomed, the keywords a position keeps, into the value
// bounds a node carrying a number as text holds none of, where numberText says
// the node carries one, which it keeps as it keeps them written beside its
// own keywords (keepTextBounds), and the rest. A type list's are kept on its
// Union, where the flat spelling keeps them on the variant carrying the text.
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

// refinedUnread returns the readDomain keywords s writes beside an allOf that
// lowered to its principal, but for those the principal already says and the
// bounds the node the position lowered to reads, which homes says
// (boundHomes.reads), where a $ref site reads no target and keeps them all
// (GitHub #1067). typed is the type of the value set the principal lowered to,
// if any (refinedValueType): a value set keeps them in its build
// (lowerPrincipalValueSet). Where standing says an inline principal's node
// stands at the position, a keyword s writes as it does is the principal's
// writing, which its node read or keeps.
func refinedUnread(c lowering.Ctx, r refinement, s *statement.Schema, typed setType, homes boundHomes, standing bool) []string {
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
	unread := unreadKeys(s, homes.reads(s)|readElsewhere(s)|held)
	if !standing {
		return unread
	}
	bs := r.principal.schema.Schema
	return slices.DeleteFunc(unread, func(key string) bool { return keyWrittenAlike(s, bs, key) })
}

// keptUnion returns the union keywords s writes beside an allOf that lowered to
// its principal r, a keyword beside a refinement, that the position keeps: none
// the principal says (unionHeld) but a union of a pair (writesUnionPair), as
// validation logic those whose branches declare no shape, as the same union
// beside any body is, and every other one degraded
// (docs/compilers/openapi.md#allof-refinement).
func keptUnion(c lowering.Ctx, r refinement, s *statement.Schema) (degraded, validation []string) {
	if !declaresUnion(s) {
		return nil, nil
	}
	shapeless := !unionBranchesDeclareShape(c, s)
	pair := writesUnionPair(s, r.comp)
	w := newAdmissionReader(c)
	for _, key := range []string{"oneOf", "anyOf"} {
		switch {
		case !hasKey(s, key) || !pair && unionHeld(&w, r, s, key):
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
// owns, nil where it owns none (refinedShape), and held what the readers of
// an inline principal standing there keep after it, which it answers where it
// keeps that principal whole (wholePrincipal). A branch the IR cannot hold is
// reported by the keeper, and nothing here claims it was kept.
func (k outcomeKeeps) record(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, pointer jsontext.Pointer,
	standing ir.TypeDef, held *principalKept,
) []ir.Diagnostic {
	shape := k.shape
	if k.refined {
		shape = refinedShape(c, s, k.comp, k.principal, standing, held)
	}
	keywords, tagged := k.besideTag(standing)
	diags := recordUnread(c, ts, p, s, keywords, shape, pointer)
	diags = append(diags, keepTagBeside(c, ts, p, s, tagged, pointer)...)
	diags = append(diags, keepTextBounds(c, p, s, k.text, pointer)...)
	for _, key := range k.validation {
		diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, s, key, pointer, c.Place())...)
	}
	diags = append(diags, recordNullBranches(c, p, k.nulls)...)
	kept, keptDiags := keepRefConjuncts(c, p, k.refEntries, pointer)
	diags = append(diags, keptDiags...)
	for _, b := range k.refEntries {
		if kept[b.entry.pointer] {
			diags = append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, b.entry.pointer, "%s", keptWhy(k.comp, b)))
		}
	}
	report := func(e keptEntry) ir.Diagnostic {
		return c.DiagAt(ir.SeverityInfo, wholeCode(e), e.entry.pointer, "%s", keptEntryWhy(k.comp, e, k.principal == nil, shape))
	}
	// The entries the position lowers from are settled first, outermost
	// first, each held against what those around it keep; their reports stand
	// where the entries do.
	lowered := k.lowered()
	settled := make([][]ir.Diagnostic, len(lowered))
	whole := false
	for j, i := range lowered {
		e, heldDiags := wholePrincipal(c, ts, p, s, k, k.entries[i], held, whole)
		whole = whole || e.keptWhole()
		settled[j] = append(heldDiags, recordKeptEntry(c, ts, p, k.comp, e, shape, false, report)...)
	}
	for i, e := range k.entries {
		if j := slices.Index(lowered, i); j >= 0 {
			diags = append(diags, settled[j]...)
			continue
		}
		diags = append(diags, recordKeptEntry(c, ts, p, k.comp, e, shape, false, report)...)
	}
	dropFoldedIntoWhole(c, p)
	return diags
}

// lowered returns the indices of k's entries the position lowers from
// (lowersFrom), the outermost first: a wrapper before what its allOf holds.
func (k outcomeKeeps) lowered() []int {
	var out []int
	for i, e := range k.entries {
		if lowersFrom(k.principal, e.entry) {
			out = append(out, i)
		}
	}
	slices.SortStableFunc(out, func(a, b int) int {
		return cmp.Compare(len(k.entries[a].entry.pointer), len(k.entries[b].entry.pointer))
	})
	return out
}

// wholePrincipal settles e, what the position s keeps of an entry it lowers
// from (lowersFrom), with held, what its principal's readers keep after the
// position (principalKept). Kept whole, the entry holds the principal and what
// they said: its report names that too, its reason reads it and what the build
// kept, and held is told, so they keep none; inside says a wrapper around e is,
// which holds e. An entry no JSON holds is not kept whole: what it keeps is
// placed keyword by keyword, as keptEntry.apart is, under its own key where p
// holds none, else the entry's.
func wholePrincipal(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, k outcomeKeeps, e keptEntry,
	held *principalKept, inside bool,
) (keptEntry, []ir.Diagnostic) {
	if inside {
		return keptEntry{entry: e.entry}, nil
	}
	if e.keptWhole() {
		if _, err := annotation.RawFromNode(e.entry.schema.Node); err != nil {
			// The keeper of the keyword no JSON holds refuses it, where written.
			e.whole, e.apart = keptApart, keptOf(c, e)
		}
	}
	if e.keptWhole() {
		if held == nil {
			// No reader of the principal keeps a keyword after the position.
			return e, nil
		}
		held.whole = true
		for _, key := range held.keys {
			if !slices.Contains(e.residue, key) {
				e.residue = append(slices.Clip(e.residue), key)
			}
		}
		if len(held.keys) > 0 {
			e.reason = sharedOf(e.reason, held.reason)
		}
		if held.built != "" {
			e.reason = sharedOf(e.reason, held.built)
		}
		return e, nil
	}
	if len(e.apart) == 0 {
		return e, nil
	}
	// The principal's readers add to its entry's object after the position; a
	// wrapper's holds what the wrapper alone keeps, held against what the
	// position writes even where the principal keeps it after (besideHeld).
	into, view := &principalKept{}, besideHeld(*p, s, e.apart)
	if e.entry.pointer == k.principal.pointer {
		held = held.saying(keeping{})
		into, view = held, *p
	}
	bs := e.entry.schema.Schema
	plain, apart := heldAgainst(view, k.comp, bs, e.entry.pointer, e.apart)
	own := slices.Concat(e.own, plain)
	e.own = slices.DeleteFunc(slices.Clone(e.residue), func(key string) bool { return !slices.Contains(own, key) })
	i := slices.IndexFunc(k.comp.entries, func(b allOfEntry) bool { return b.pointer == e.entry.pointer })
	return e, keepApart(c, ts, k.comp, into, p, bs, e.entry.pointer, apart, apartReason(c, s, k.comp, i, e, apart))
}

// apartReason is the reason the keywords apart, kept under the entry's key of
// e, the inline entry at index i of comp, share, else degraded_lowering: each
// as it is kept on its own (keywordReason), and a value bound where the
// position's node carries a number as text no_ir_home, as recordOwnKeywords
// keeps one (keepTextBounds).
func apartReason(c lowering.Ctx, s *statement.Schema, comp allOfComposition, i int, e keptEntry, apart []string) ir.UnmodeledReason {
	var shared ir.UnmodeledReason
	for n, key := range apart {
		reason := keywordReason(c, s, comp, i, key)
		if e.numberText && slices.Contains(valueConstraintKeywords, key) {
			reason = ir.ReasonNoIRHome
		}
		if n > 0 {
			reason = sharedOf(shared, reason)
		}
		shared = reason
	}
	return shared
}

// besideTag splits k's unread keywords where standing, the node the position
// owns, is the Union its inline principal built holding that principal's
// discriminator: the position's own discriminator is kept beside one the node
// holds, never for want of a home, so it is reported apart (keepTagBeside).
func (k outcomeKeeps) besideTag(standing ir.TypeDef) (keywords, tagged []string) {
	u, ok := standing.(*ir.Union)
	if !ok || u.Discriminator == nil || !slices.Contains(k.keywords, "discriminator") {
		return k.keywords, nil
	}
	return slices.DeleteFunc(slices.Clone(k.keywords), func(key string) bool { return key == "discriminator" }), []string{"discriminator"}
}

// keepTagBeside keeps on p the discriminator s, written at pointer, writes
// beside an allOf whose inline principal's Union holds its own, tagged naming
// it or nothing: verbatim, its routes resolved, as keepUnreadKeys keeps one, with
// a report saying whose discriminator the Union holds.
func keepTagBeside(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, s *statement.Schema, tagged []string, pointer jsontext.Pointer,
) []ir.Diagnostic {
	kept, diags := keepUnreadKeys(c, ts, p, s, tagged, pointer)
	if len(kept) == 0 {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer,
		"the Union this position lowered to holds the discriminator its inline allOf entry writes, and one "+
			"Union is routed by one; the discriminator declared beside the allOf is kept verbatim under Unmodeled"))
}

// keptRef is the key a kept $ref conjunct is kept under, and where the entry
// is said to be written.
type keptRef struct {
	key string
	at  jsontext.Pointer
}

// keptRefKeys returns, by its pointer, the key each $ref of refEntries is kept
// under: openapi:$ref where one is kept, else openapi:$ref/<i>, i its rank by
// reference text, then by the entry as written, so no order of the entries
// decides (docs/compilers/openapi.md#allof-refinement). Entries written alike
// are one entry, at pointer, the position writing them.
func keptRefKeys(refEntries []wholeRefEntry, pointer jsontext.Pointer) map[jsontext.Pointer]keptRef {
	alike := map[[2]string][]jsontext.Pointer{}
	for _, b := range refEntries {
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
	out := make(map[jsontext.Pointer]keptRef, len(refEntries))
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

// keepRefConjuncts keeps on p each $ref conjunct of refEntries, one a
// refinement, a composition or a failed read leaves unlowered: the reference
// as written, its text alone, under the key keptRefKeys ranks it by, at the
// entry writing it. What an entry writes beside its $ref is kept apart, as an
// inline entry's keywords are. It reports which it kept, by pointer.
func keepRefConjuncts(c lowering.Ctx, p *ir.Unmodeled, refEntries []wholeRefEntry, pointer jsontext.Pointer,
) (map[jsontext.Pointer]bool, []ir.Diagnostic) {
	var keys map[jsontext.Pointer]keptRef
	if len(refEntries) == 1 {
		// One kept $ref is openapi:$ref at its entry, as keptRefKeys ranks it.
		keys = map[jsontext.Pointer]keptRef{refEntries[0].entry.pointer: {key: "openapi:$ref", at: refEntries[0].entry.pointer}}
	} else {
		keys = keptRefKeys(refEntries, pointer)
	}
	kept := make(map[jsontext.Pointer]bool, len(refEntries))
	var diags []ir.Diagnostic
	for _, b := range refEntries {
		_, text := refSpelling(b.entry.schema)
		ref := keys[b.entry.pointer]
		ok, keptDiags := annotation.PreserveNodeInto(p, ref.key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: text},
			ir.ReasonDegradedLowering, c.Place().At(ref.at))
		diags = append(diags, keptDiags...)
		kept[b.entry.pointer] = ok
	}
	return kept, diags
}

// keptWhy words why a refinement keeps a $ref entry, naming the schema
// writing its conjuncts as comp's subject does.
func keptWhy(comp allOfComposition, b wholeRefEntry) string {
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
		e := allOfEntry{schema: c.Statements().Read(s.BesideRef), pointer: pointer}
		kept, keptDiags := keepRefConjuncts(c, p, []wholeRefEntry{{entry: e}}, pointer)
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
func refConjuncts(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer) ([]wholeRefEntry, bool) {
	entries := allOfEntries(c, s, pointer).entries
	refs := make([]wholeRefEntry, 0, len(entries))
	for _, e := range entries {
		if e.schema.Ref == nil {
			return nil, false
		}
		refs = append(refs, wholeRefEntry{entry: e})
	}
	return refs, len(refs) > 0
}
