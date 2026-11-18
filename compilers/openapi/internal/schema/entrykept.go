// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"encoding/json/jsontext"
	"maps"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/promotion"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// keptEntry is what one inline allOf entry keeps verbatim beside what its
// position lowered to (docs/compilers/openapi.md#allof-inline-residue): each
// keyword nothing read, under the key and reason it has beside the allOf, or
// the entry whole, where a keyword of it is read against the rest of it or
// another conjunct writes it otherwise.
type keptEntry struct {
	entry allOfBranch
	// residue is what the entry writes that nothing read, in the order written.
	residue []string
	// own is what of residue the entry keeps under its own keys: residue less
	// what a rule consumes and what the position writes alike, which keeps it.
	own []string
	// alike is what of own another inline entry keeps alike, kept once, at the
	// position's pointer.
	alike []string
	// validation is what of own is a union of validation logic
	// (entryUnionReason).
	validation []string
	// whole is why the entry is kept whole, keptApart where it is not.
	whole wholeCause
	// reason is what a whole entry is kept as (sharedReason).
	reason ir.UnmodeledReason
	// numberText says the position's own node carries a number as text, whose
	// Constraints hold no value bound own keeps (keepTextBounds).
	numberText bool
}

// keeps reports whether k keeps anything.
func (k keptEntry) keeps() bool { return k.keptWhole() || len(k.own) > 0 }

// keptWhole reports whether k is kept whole, holding what its entry writes: it
// is no writer of a keyword it writes alike with another conjunct, kept apart
// or folded, which that conjunct keeps alone (annotation.Voice.KeptWhole).
func (k keptEntry) keptWhole() bool { return k.whole != keptApart }

// wholeCause is why an inline allOf entry is kept whole.
type wholeCause uint8

const (
	// keptApart marks an entry keeping each keyword under its own key.
	keptApart wholeCause = iota
	// wholeClosure marks one writing a closure the position does not type, or
	// an unevaluated keyword: each reads the keywords beside it in the entry.
	wholeClosure
	// wholeUnflattened marks one whose own allOf is not flattened: past the
	// flattening depth, or empty.
	wholeUnflattened
	// wholeElsewhere marks one reading a relative reference against another
	// base than the position does.
	wholeElsewhere
	// wholeDisagreed marks one writing what folds, which another conjunct
	// writes otherwise (voicesAmong).
	wholeDisagreed
	// wholeApart marks one keeping a keyword another conjunct writes otherwise,
	// which one entry under that keyword cannot hold.
	wholeApart
)

// entryKeeps decides what each inline entry of comp, the allOf s writes, keeps
// of what residueOf says it left unread, by index: nothing where that is
// nothing a rule does not consume, else the entry whole (wholeCauseOf), where
// closure names a keyword the position types no closure from, else each
// keyword apart. v is s's voicing, nil where its allOf does not decide.
func entryKeeps(c lowering.Ctx, s *statement.Schema, comp allOfComposition, residueOf func(int) []string,
	v voicing, closure func(string) bool,
) []keptEntry {
	out := make([]keptEntry, len(comp.branches))
	for i := range comp.branches {
		out[i] = keptEntry{entry: comp.branches[i], residue: residueOf(i)}
	}
	kepts := make([][]string, len(out))
	for i, k := range out {
		kepts[i] = keptOf(c, k)
	}
	apart, apartKept := writtenApartKeys(c, s, comp, kepts, new(budget.Budget))
	held := map[string]int{}
	for i, k := range out {
		kept := kepts[i]
		if len(kept) == 0 {
			continue
		}
		apartHere := apart
		if failedRef(c, k.entry) {
			apartHere = apartKept
		}
		out[i].whole = wholeCauseOf(c, comp, i, k.residue, v, closure, kept, apartHere)
		if out[i].keptWhole() {
			out[i].reason = sharedReason(c, s, comp, i, kept)
			continue
		}
		out[i].own = slices.DeleteFunc(kept, func(key string) bool { return hasKey(s, key) })
		for _, key := range out[i].own {
			held[key]++
			if (key == "oneOf" || key == "anyOf") && entryUnionReason(c, s, comp, i) == ir.ReasonValidationOnly {
				out[i].validation = append(out[i].validation, key)
			}
		}
	}
	for i := range out {
		out[i].alike = slices.DeleteFunc(slices.Clone(out[i].own), func(key string) bool { return held[key] < 2 })
	}
	return out
}

// markKeptWhole marks each of voices, the voices of s written at pointer, which
// lowered to target, whose branch the outcome keeps whole (keptWhole), so
// annotation.Read counts it as no writer of what it folds, as entryKeeps
// counts no such entry as a writer of what it keeps: the entry holds it once.
// One voice writes no keyword alike with another, so fewer than two stand.
func markKeptWhole(c lowering.Ctx, s *statement.Schema, pointer jsontext.Pointer, target ir.TypeRef,
	voices []annotation.Voice,
) []annotation.Voice {
	if len(voices) < 2 {
		return voices
	}
	var kept []keptEntry
	if _, refined := refinementOf(c, s, pointer); refined {
		kept = keepsAtOutcome(c, s, target, pointer).entries
	} else {
		kept = readAllOf(c, s, pointer).kept
	}
	whole := map[jsontext.Pointer]bool{}
	for _, k := range kept {
		if k.keptWhole() {
			whole[k.entry.pointer] = true
		}
	}
	if len(whole) == 0 {
		return voices
	}
	out := slices.Clone(voices)
	for i, v := range out {
		out[i].KeptWhole = whole[v.At]
	}
	return out
}

// writtenApartKeys returns which of the keywords the entries of comp keep, kepts
// by index, their writers write apart: otherwise than one another, or in a
// value that cannot be rendered. The writers are s and the inline entries (and
// $refs naming nothing) writing the key; among keepers, as a $ref naming nothing
// is asked, only the entries keeping it, as no merge or fold of another entry
// parts it. Each writer is read once whatever it keeps: reads is charged a step
// for each key asked of.
func writtenApartKeys(c lowering.Ctx, s *statement.Schema, comp allOfComposition, kepts [][]string,
	reads *budget.Budget,
) (writers, keepers map[string]bool) {
	asked, askedKeepers := writtenKeys{}, writtenKeys{}
	for i, kept := range kepts {
		failed := len(kept) > 0 && failedRef(c, comp.branches[i])
		for _, key := range kept {
			asked[key] = keyWrite{}
			if failed {
				askedKeepers[key] = keyWrite{}
			}
		}
	}
	asked.readWriter(s, reads)
	askedKeepers.readWriter(s, reads)
	for i, e := range comp.branches {
		if (e.schema.Ref != nil && !failedRef(c, e)) || !stated(e.schema.Schema) {
			continue
		}
		asked.readWriter(e.schema.Schema, reads)
		askedKeepers.readKept(e.schema.Schema, kepts[i], reads)
	}
	return asked.apart(), askedKeepers.apart()
}

// keyWrite is what the writers of one key met so far write: the first one's
// rendering, and whether any writer wrote it otherwise or could not be
// rendered, which no later writer changes.
type keyWrite struct {
	first ir.RawValue
	met   bool
	apart bool
}

// writtenKeys maps each key asked of to what its writers met so far write.
type writtenKeys map[string]keyWrite

// readWriter notes of each key bs writes that w asks of, what it writes.
func (w writtenKeys) readWriter(bs *statement.Schema, reads *budget.Budget) {
	if len(w) == 0 {
		return
	}
	for _, k := range bs.Keys {
		reads.Spend(1)
		if _, asked := w[k.Name]; asked {
			w.note(bs, k.Name)
		}
	}
}

// readKept notes of each of kept, the keys an entry bs keeps, that w asks of,
// what bs writes, where it writes it.
func (w writtenKeys) readKept(bs *statement.Schema, kept []string, reads *budget.Budget) {
	if len(w) == 0 {
		return
	}
	for _, key := range kept {
		reads.Spend(1)
		if _, asked := w[key]; asked {
			w.note(bs, key)
		}
	}
}

// note records what bs writes for key, unless it writes none (hasKey): the
// first writer's rendering is kept and each later one is compared with it, and
// a key already apart is read no further.
func (w writtenKeys) note(bs *statement.Schema, key string) {
	k, read := bs.Key(key)
	if !read {
		return
	}
	seen := w[key]
	if seen.apart {
		return
	}
	raw, err := annotation.RawFromNode(k.Node)
	switch {
	case err != nil || (seen.met && !bytes.Equal(seen.first, raw)):
		seen = keyWrite{met: true, apart: true}
	case !seen.met:
		seen = keyWrite{first: raw, met: true}
	}
	w[key] = seen
}

// apart returns which of the keys asked of their writers write apart.
func (w writtenKeys) apart() map[string]bool {
	out := make(map[string]bool, len(w))
	for key, seen := range w {
		out[key] = seen.apart
	}
	return out
}

// keptOf is what of k's residue a rule does not consume (consumedByRule):
// what the entry keeps, whole or apart.
func keptOf(c lowering.Ctx, k keptEntry) []string {
	if len(k.residue) == 0 {
		return nil
	}
	bs := k.entry.schema.Schema
	return slices.DeleteFunc(slices.Clone(k.residue), func(key string) bool {
		return consumedByRule(c, bs, key, k.entry.pointer)
	})
}

// consumedByRule reports whether key, a key bs writes at pointer, is one the
// dialect reads as a target or drops, beside an allOf and on its entry alike:
// $comment, $anchor, $dynamicAnchor and $defs, or a draft's twin of one, but
// where the dialect bs is read in defines no such keyword
// (docs/compilers/openapi.md#dialect-keywords).
func consumedByRule(c lowering.Ctx, bs *statement.Schema, key string, pointer jsontext.Pointer) bool {
	switch TwinKeyword(c, bs, key) {
	case "$comment", "$anchor", "$dynamicAnchor", "$defs":
		return !writesUnknown(c, bs, key, pointer)
	default:
		return false
	}
}

// wholeCauseOf returns why the inline entry at index i of comp, leaving residue
// unread, of which it keeps kept, is kept whole, and keptApart where it keeps
// each keyword apart. apart says which keywords their writers write apart.
func wholeCauseOf(c lowering.Ctx, comp allOfComposition, i int, residue []string, v voicing,
	closure func(string) bool, kept []string, apart map[string]bool,
) wholeCause {
	e := comp.branches[i]
	switch {
	case slices.ContainsFunc(residue, closure) || slices.ContainsFunc(residue, unevaluatedKeyword):
		return wholeClosure
	case e.capped || slices.Contains(residue, "allOf"):
		return wholeUnflattened
	case v.rebased != nil && v.rebased[i] || readsElsewhere(c, comp, e, kept):
		return wholeElsewhere
	case e.schema.Ref == nil && (v.speaks == nil || !v.speaks[i]) && disagreed(c, e.schema.Schema, residue):
		return wholeDisagreed
	case slices.ContainsFunc(kept, func(key string) bool { return apart[key] }):
		return wholeApart
	default:
		return keptApart
	}
}

// failedRef reports whether e is a $ref entry whose reference lands on no
// schema, as resolveSchemaRef finds, a loop of $refs among them: no node stands
// for it, so nothing reads what it writes.
func failedRef(c lowering.Ctx, e allOfBranch) bool {
	return e.schema.Ref != nil && !namesReferent(c, e.schema)
}

// failedRefResidue is what the $ref entry e writes that nothing reads, in the
// order written, where its reference names nothing, else nil: every keyword
// beside the reference but `required`, which the composition's members read
// (compositionRequired). Where the reference resolves, its target's alias homes
// the keywords beside it (fillRefBranch).
func failedRefResidue(c lowering.Ctx, e allOfBranch) []string {
	if !failedRef(c, e) {
		return nil
	}
	keyword, _ := refSpelling(e.schema)
	return unmergedBranchKeys(e.schema.Schema, func(key string) bool { return key == keyword || key == "required" })
}

// unevaluatedKeyword reports whether keyword is an unevaluated one, which on an
// entry holds the instance to what that entry alone evaluates.
func unevaluatedKeyword(keyword string) bool {
	return keyword == "unevaluatedProperties" || keyword == "unevaluatedItems"
}

// readsElsewhere reports whether a keyword the inline entry e of comp keeps,
// of residue, writes a relative reference at any depth, read against another
// base than the position's: an identifier at or above the entry names another
// URI (underID, readsAtPosition), so the reference, kept apart from that
// identifier, would name another schema. What the entry merges in place is
// lowered where it is written, against its own base.
func readsElsewhere(c lowering.Ctx, comp allOfComposition, e allOfBranch, residue []string) bool {
	bs := e.schema.Schema
	if !underID(e, bs, idWrappers(comp)) {
		return false
	}
	return slices.ContainsFunc(residue, func(key string) bool {
		raw, err := annotation.RawFromNode(keyNode(bs, key))
		return err != nil || annotation.WritesReference(raw)
	}) && !readsAtPosition(c, e)
}

// sharedReason is the reason the keywords kept of the inline entry at index i
// of comp, the allOf s writes, share where they share one, else
// degraded_lowering: what a consumer filtering by reason reads of the entry
// kept whole.
func sharedReason(c lowering.Ctx, s *statement.Schema, comp allOfComposition, i int, kept []string) ir.UnmodeledReason {
	var shared ir.UnmodeledReason
	for n, key := range kept {
		reason := keywordReason(c, s, comp, i, key)
		if n > 0 && reason != shared {
			return ir.ReasonDegradedLowering
		}
		shared = reason
	}
	return shared
}

// logicKeywords are the validation-only keywords a schema keeps under
// ReasonValidationOnly (docs/compilers/openapi.md#validation-only), as their
// 2020-12 twins (TwinKeyword).
var logicKeywords = []string{
	"not", "if", "then", "else", "dependentSchemas", "dependentRequired", "propertyNames",
	"contains", "minContains", "maxContains",
}

// keywordReason is the reason key, a keyword the inline entry at index i of
// comp, the allOf s writes, writes, is kept under on its own, as beside the
// allOf: its slot's (docs/compilers/openapi.md#allof-inline-residue).
func keywordReason(c lowering.Ctx, s *statement.Schema, comp allOfComposition, i int, key string) ir.UnmodeledReason {
	e := comp.branches[i]
	bs := e.schema.Schema
	twin := TwinKeyword(c, bs, key)
	switch {
	case strings.HasPrefix(key, "x-"):
		return ir.ReasonVendorExtension
	case annotation.DialectKeyword(twin), writesUnknown(c, bs, key, e.pointer):
		return ir.ReasonOutOfScope
	case slices.Contains(logicKeywords, twin), unevaluatedKeyword(twin):
		return ir.ReasonValidationOnly
	case key == "oneOf" || key == "anyOf":
		return entryUnionReason(c, s, comp, i)
	case slices.Contains(contentKeywords, key):
		return ir.ReasonNoIRHome
	default:
		return ir.ReasonDegradedLowering
	}
}

// entryUnionReason is the reason the oneOf or anyOf the inline entry at index
// i of comp, the allOf s writes, is kept under, as the union beside s would
// be: validation logic where its branches declare no shape against s, and
// degraded where they do or the union shares no kind with the rest
// (docs/compilers/openapi.md#union-beside-structure).
func entryUnionReason(c lowering.Ctx, s *statement.Schema, comp allOfComposition, i int) ir.UnmodeledReason {
	holder := comp.branches[i].schema.Schema
	choice := holder.ChoiceKeywords()
	if unionApart(c, s, comp, i) || setsDeclareShape(c, s, holder, choice.OneOf, choice.AnyOf) {
		return ir.ReasonDegradedLowering
	}
	return ir.ReasonValidationOnly
}

// unionApart reports whether the oneOf and anyOf the inline entry at index i
// of comp writes share no kind with what the rest of s, the schema writing
// comp, admits, the entry's other keywords among it: no instance satisfies
// them together, as admissionReader.conjuncts reads a union beside the allOf. A
// read cut short says no.
func unionApart(c lowering.Ctx, s *statement.Schema, comp allOfComposition, i int) bool {
	w := newAdmissionReader(c)
	bs := comp.branches[i].schema.Schema
	union := unionsIn(&w, bs)
	rest := w.written(s, w.nullable).and(w.written(bs, w.nullable))
	if declaresUnion(s) {
		rest = rest.and(unionsIn(&w, s))
	}
	for j, e := range comp.branches {
		switch {
		case j == i || !stated(e.schema.Schema):
		case e.schema.Ref != nil:
			rest = rest.and(w.of(e.schema))
		default:
			rest = rest.and(inlineConjunct(&w, e.schema.Schema))
		}
	}
	return !w.incomplete() && rest.reachable() && union.kinds&^kindNull != 0 && rest.kinds&union.kinds == 0
}

// recordKeptEntry keeps k, an inline entry of comp, on p, beside what the allOf
// lowered to, shape, and reports it: whole, under the entry's key and k.reason,
// reported by report, or each keyword apart (recordOwnKeywords), warn turning
// the report of what has no home into a warning. A keyword other inline
// entries keep alike takes the provenance of the position comp is written at.
func recordKeptEntry(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, comp allOfComposition,
	k keptEntry, shape string, warn bool, report func(keptEntry) ir.Diagnostic,
) []ir.Diagnostic {
	if !k.keeps() {
		return nil
	}
	e := k.entry
	if k.keptWhole() {
		kept, diags := annotation.PreserveNodeInto(p, comp.branchKey(e.pointer), e.schema.Node, k.reason, c.Place().At(e.pointer))
		if !kept {
			return diags
		}
		return append(diags, report(k))
	}
	var held ir.Unmodeled
	diags := recordOwnKeywords(c, ts, &held, k, shape, warn)
	for _, key := range k.alike {
		if entry, ok := held[annotation.EntryKey("", key)]; ok {
			entry.Provenance = c.Place().At(comp.pointer)
			held[annotation.EntryKey("", key)] = entry
		}
	}
	return append(diags, mergeKept(c, p, held, e.pointer)...)
}

// dropFoldedIntoWhole removes from p each entry an allOf entry kept whole
// holds, what its fold kept beside it, so that is kept once, inside the entry
// (docs/ir/unmodeled.md#keys); a key the promotion policy reads stays. An entry
// kept whole within another stays under its own path too, as
// docs/compilers/openapi.md#allof-inline-residue keeps it.
func dropFoldedIntoWhole(c lowering.Ctx, p *ir.Unmodeled) {
	var whole ir.Unmodeled
	for key, entry := range *p {
		if strings.HasPrefix(key, allOfKeyPrefix) && !writesTopLevelRef(entry.Value) {
			whole = annotation.MergeUnmodeled(whole, ir.Unmodeled{key: entry})
		}
	}
	if len(whole) == 0 {
		return
	}
	promotion.DropContained(c.Promoter(), p, slices.Collect(maps.Keys(whole))...)
	maps.Copy(*p, whole)
}

// writesTopLevelRef reports whether raw, a kept entry's value, is an object
// naming a $ref member: a $ref entry an allOf keeps whole, which folds nothing,
// where an inline one writes none. Asking only inline entries what they hold
// keeps the question linear in an allOf of many $refs.
func writesTopLevelRef(raw ir.RawValue) bool {
	dec := jsontext.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return false
	}
	for dec.PeekKind() == '"' {
		name, err := dec.ReadToken()
		if err != nil {
			return false
		}
		if name.String() == "$ref" {
			return true
		}
		if dec.SkipValue() != nil {
			return false
		}
	}
	return false
}

// allOfKeyPrefix opens the key of an allOf entry kept whole (branchKey).
const allOfKeyPrefix = "openapi:allOf/"

// mergeKept writes held, what an inline entry at pointer keeps apart, onto p.
// No other reader keeps one of its keys otherwise (entryKeeps), so a key held
// with another value is an invariant broken, reported, never overwritten.
func mergeKept(c lowering.Ctx, p *ir.Unmodeled, held ir.Unmodeled, pointer jsontext.Pointer) []ir.Diagnostic {
	var diags []ir.Diagnostic
	for _, key := range slices.Sorted(maps.Keys(held)) {
		if other, taken := (*p)[key]; taken && !bytes.Equal(other.Value, held[key].Value) {
			diags = append(diags, c.DiagAt(ir.SeverityError, diag.InternalInvariant, pointer,
				"internal: %s, kept apart from this allOf branch, is held with another value", key))
			continue
		}
		if *p == nil {
			*p = make(ir.Unmodeled, len(held))
		}
		(*p)[key] = held[key]
	}
	return diags
}

// recordOwnKeywords keeps each keyword of k.own verbatim on p, under its own
// key and the reason its slot gives, and reports them as beside the allOf: an
// unknown, content, dialect or dynamic reference keyword at the keyword, a
// vendor extension in no finding, a union of validation logic at the entry,
// and the rest in one finding at the entry (recordEntryUnhomed).
func recordOwnKeywords(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, k keptEntry, shape string, warn bool,
) []ir.Diagnostic {
	bs, at := k.entry.schema.Schema, k.entry.pointer
	var diags []ir.Diagnostic
	var unknown, unhomed, text []string
	for _, key := range k.own {
		switch twin := TwinKeyword(c, bs, key); {
		case k.numberText && slices.Contains(valueConstraintKeywords, key):
			text = append(text, key)
		case annotation.DialectKeyword(twin):
			// Ahead of the unknown keywords, as Read keeps $vocabulary before
			// PreserveUnknownKeywords meets it.
			dialect := dialectIn(c.Rules().At(c.Position(at)), bs)
			diags = append(diags, annotation.KeepDialectKeyword(p, bs, key, at, c.Place(), dialect)...)
		case strings.HasPrefix(key, "x-"):
			_, keptDiags := annotation.PreserveSchemaKeyword(p, bs, key, ir.ReasonVendorExtension, at, c.Place())
			diags = append(diags, keptDiags...)
		case writesUnknown(c, bs, key, at):
			unknown = append(unknown, key)
		case slices.Contains(contentKeywords, key):
			diags = append(diags, keepContentKeyword(c, p, bs, key, at)...)
		case slices.Contains(k.validation, key):
			diags = append(diags, annotation.PreserveValidationOnlyKeyword(p, bs, key, at, c.Place())...)
		case key == "$dynamicRef" || key == "$recursiveRef":
			diags = append(diags, keepEntryDynamicRef(c, p, bs, key, at)...)
		default:
			unhomed = append(unhomed, key)
		}
	}
	diags = append(diags, keepUnknownOf(c, p, bs, unknown, at)...)
	diags = append(diags, keepTextBounds(c, p, bs, text, at)...)
	where := "on this inline branch"
	switch {
	case k.entry.schema.Ref != nil:
		where = "beside this $ref, which names nothing"
	case besideRef(k.entry):
		where = "beside this $ref"
	}
	return append(diags, recordEntryUnhomed(c, ts, p, k.entry, unhomed, shape, where, warn)...)
}

// besideRef reports whether e is what an allOf entry writing a $ref beside
// other keys writes beside it (statement.Schema.WrittenBeside): a conjunct of
// its own, kept as an inline entry's is
// (docs/compilers/openapi.md#allof-classification), though no inline entry.
func besideRef(e allOfBranch) bool { return e.schema.WrittenBeside != nil }

// branchNames is how a report names an allOf entry: at the head of a sentence
// (lead), as the entry it is about (this), and as what it keeps (kept).
type branchNames struct{ lead, this, kept string }

// namesOf returns how a report names e: as an inline branch, or as the
// conjunct written beside its $ref where it is one (besideRef).
func namesOf(e allOfBranch) branchNames {
	if besideRef(e) {
		return branchNames{lead: "conjunct beside this allOf branch's $ref", this: "the conjunct beside this $ref",
			kept: "the conjunct"}
	}
	return branchNames{lead: "inline allOf branch", this: "this inline branch", kept: "the branch"}
}

// keepEntryDynamicRef keeps the dynamic reference keyword the inline entry bs
// at pointer writes verbatim on p, as degraded_lowering, and reports it at the
// keyword, as recordUnexpandedDynamicRef reports one beside the allOf: no entry
// expands one, as it is a conjunct the IR cannot intersect with the rest.
func keepEntryDynamicRef(c lowering.Ctx, p *ir.Unmodeled, bs *statement.Schema, keyword string, pointer jsontext.Pointer) []ir.Diagnostic {
	kept, diags := annotation.PreserveSchemaKeyword(p, bs, keyword, ir.ReasonDegradedLowering, pointer, c.Place())
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, pointer+ids.Ptr(keyword),
		"%s was not expanded because it is written on an allOf entry beside another conjunct, which the IR "+
			"cannot intersect with it; it is kept verbatim under Unmodeled", keyword))
}

// keepUnknownOf keeps the keywords of keys, each one bs, the schema at pointer,
// writes that PreserveUnknownKeywords keeps (writesUnknown), as it keeps them,
// and none of the others bs writes.
func keepUnknownOf(c lowering.Ctx, p *ir.Unmodeled, bs *statement.Schema, keys []string, pointer jsontext.Pointer) []ir.Diagnostic {
	if len(keys) == 0 {
		return nil
	}
	var others []string
	for _, k := range bs.Keys {
		if !slices.Contains(keys, k.Name) {
			others = append(others, k.Name)
		}
	}
	undefined := slices.DeleteFunc(undefinedKeywords(c, bs), func(key string) bool { return !slices.Contains(keys, key) })
	diags := annotation.UnknownKeywordsIn(p, bs, pointer, c.Place(), others...)
	return append(diags, annotation.UndefinedKeywordsIn(p, bs, undefined, pointer, c.Place())...)
}

// recordEntryUnhomed keeps each of unhomed, keywords the entry e writes that
// no home the position lowered to, shape, holds, as degraded_lowering
// (keepUnhomed), and reports them in one finding at the entry, where says how
// they are written: a warning where warn says e declares a type no object is,
// which the composed model asserts it is.
func recordEntryUnhomed(c lowering.Ctx, ts *compile.Types, p *ir.Unmodeled, e allOfBranch, unhomed []string,
	shape, where string, warn bool,
) []ir.Diagnostic {
	kept, diags := keepUnhomed(c, ts, p, e.schema.Schema, unhomed, e.pointer)
	if len(kept) == 0 {
		return diags
	}
	if warn {
		return append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, e.pointer,
			"%s declares a type that is not an object, which the composed model asserts it is; "+
				"%s, written on it, kept verbatim under Unmodeled", namesOf(e).lead, strings.Join(kept, ", ")))
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, e.pointer,
		"this allOf lowered to %s, which has no home for %s written %s; kept verbatim under Unmodeled",
		shape, strings.Join(kept, ", "), where))
}

// wholeWhy words why k is kept whole where its cause says so whatever the
// position lowered to, and is "" where the caller's own wording says it.
func wholeWhy(k keptEntry) string {
	n := namesOf(k.entry)
	switch k.whole {
	case wholeElsewhere:
		return "this " + n.lead + " reads its $refs against another base than the position does, as an " +
			"identifier at or above it names another URI, so nothing it writes folds or is kept apart; " + n.kept +
			" (" + strings.Join(k.residue, ", ") + ") is kept verbatim under Unmodeled"
	case wholeDisagreed:
		return disagreedWhy(k.residue, n)
	case wholeApart:
		return "another conjunct of this allOf writes a keyword " + n.this + " keeps otherwise, and one entry " +
			"under that keyword holds one value; " + n.kept + " (" + strings.Join(k.residue, ", ") +
			") is kept verbatim under Unmodeled"
	default:
		return ""
	}
}

// wholeCode is the code a report of k, kept whole, takes: the one a
// validation-only keyword's takes where its reason is that, as each of its
// keywords would draw, else the one of a degraded lowering.
func wholeCode(k keptEntry) string {
	if k.reason == ir.ReasonValidationOnly {
		return diag.ValidationOnlyKeyword
	}
	return diag.DegradedConstruct
}

// keepFailedRefs keeps beside the composed model m what the $ref entries of
// comp at the indices failed, each reference of which names nothing, write
// beside the reference: each keyword as an inline entry's is kept
// (entryKeeps), or the entry whole where one is. It returns the entries kept
// apart, whose references keepUncomposedRefs keeps. The failure itself is
// reported where the reference is written (fillRefBranch), so nothing here
// reports it again.
func keepFailedRefs(c lowering.Ctx, ts *compile.Types, m *ir.Model, comp allOfComposition, failed []int,
) ([]keptBranch, []ir.Diagnostic) {
	diags := make([]ir.Diagnostic, 0, len(failed))
	var apart []keptBranch
	for _, i := range failed {
		k := comp.kept[i]
		if !k.keptWhole() {
			apart = append(apart, keptBranch{entry: k.entry})
		}
		diags = append(diags, recordKeptEntry(c, ts, &m.Unmodeled, comp, k, nodeShape(ir.KindModel), false,
			func(k keptEntry) ir.Diagnostic {
				return c.DiagAt(ir.SeverityInfo, wholeCode(k), k.entry.pointer, "%s", failedRefWholeWhy(k))
			})...)
	}
	return apart, diags
}

// failedRefWholeWhy words why the $ref entry k, naming nothing, is kept whole.
func failedRefWholeWhy(k keptEntry) string {
	return "this allOf branch's $ref names nothing, so nothing reads what it writes, and a keyword that reads the " +
		"others beside it, or that another conjunct writes otherwise, cannot be kept apart; the branch (" +
		strings.Join(k.residue, ", ") + ") is kept verbatim under Unmodeled"
}
