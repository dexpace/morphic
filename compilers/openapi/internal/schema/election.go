// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"slices"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
)

// chooseConjunctToLowerFrom picks the conjunct the position lowers from, of refs the $refs that
// may be: an inline value set within every other, since it fixes the values
// and what it narrows cannot; else a $ref, since replacing it drops what its
// target says (narrowestRef); else, of those naming a kind, the conjunct
// within the most others. A `format` or `items` naming no kind only qualifies
// one, so it never decides, and where nothing names a kind no branch does. No
// tie goes by the order written: untie breaks one, and words neither within
// the other elect none.
func (w *admissionReader) chooseConjunctToLowerFrom(conjuncts []conjunct, refs []*conjunct) *conjunct {
	for i := range conjuncts {
		if w.narrowsAll(conjuncts, i, refs) {
			return untie(w, conjuncts, strongest(narrowingSets(conjuncts, i, refs)))
		}
	}
	if len(refs) > 0 {
		return narrowestRef(w, conjuncts, refs)
	}
	tied := strongest(mostWithin(w, conjuncts))
	if len(tied) > 1 && tied[0].form == formWords && !admitAlike(w, tied) {
		return &conjuncts[0]
	}
	return untie(w, conjuncts, tied)
}

// narrowingSets returns the value sets within every other conjunct, the first
// of them at i: those after it whose kinds and values are its own, as each
// set within every other is within the first, and the first within it.
func narrowingSets(conjuncts []conjunct, i int, refs []*conjunct) []*conjunct {
	first := &conjuncts[i]
	sets := []*conjunct{first}
	for j := i + 1; j < len(conjuncts); j++ {
		cj := &conjuncts[j]
		if cj.read.fixed && !slices.Contains(refs, cj) && sameValues(cj.read, first.read) {
			sets = append(sets, cj)
		}
	}
	return sets
}

// sameValues reports whether a and b, two value sets, admit the same kinds and
// hold the same members.
func sameValues(a, b admitted) bool {
	return a.kindsWithin(b) && b.kindsWithin(a) && a.valuesWithin(b) && b.valuesWithin(a)
}

// mostWithin returns the conjuncts naming a kind that lie within the most
// conjuncts, in the order written.
func mostWithin(admissionReaderValue *admissionReader, conjuncts []conjunct) []*conjunct {
	var tied []*conjunct
	most := 0
	for i := range conjuncts {
		cj := &conjuncts[i]
		if !cj.says {
			continue
		}
		switch n := admissionReaderValue.conesOver(conjuncts, cj.read); {
		case n > most:
			tied, most = append(tied[:0], cj), n
		case n == most:
			tied = append(tied, cj)
		}
	}
	return tied
}

// strongest returns those of tied whose form ranks first (conjunctForm).
func strongest(tied []*conjunct) []*conjunct {
	top := formWords
	for _, cj := range tied {
		top = max(top, cj.form)
	}
	return slices.DeleteFunc(tied, func(cj *conjunct) bool { return cj.form != top })
}

// admitAlike reports whether each of tied lies within every other: what they admit
// is one, so far as a reading holds it.
func admitAlike(admissionReaderValue *admissionReader, tied []*conjunct) bool {
	first := tied[0].read
	return !slices.ContainsFunc(tied[1:], func(cj *conjunct) bool {
		return !admissionReaderValue.within(first, cj.read) || !admissionReaderValue.within(cj.read, first)
	})
}

// untie returns which of tied, conjuncts of one form, the position lowers
// from: the one there is; of words admitting alike, the pick untieWords makes;
// of unions of one keyword, the first where each is that one schema written
// again, else none, as no rule ranks their branches, and the keywords beside
// the allOf decide (docs/compilers/openapi.md#allof-refinement). A tie of no
// conjunct leaves those keywords too.
func untie(admissionReaderValue *admissionReader, conjuncts []conjunct, tied []*conjunct) *conjunct {
	switch {
	case len(tied) == 0:
		return &conjuncts[0]
	case len(tied) == 1:
		return tied[0]
	case tied[0].form == formWords:
		return untieWords(admissionReaderValue, &conjuncts[0], tied)
	case writtenAlike(admissionReaderValue, tied):
		return tied[0]
	default:
		return &conjuncts[0]
	}
}

// writtenAlike reports whether each of tied is an allOf branch written as the
// first is (sameText): one schema written several times.
func writtenAlike(admissionReaderValue *admissionReader, tied []*conjunct) bool {
	return !slices.ContainsFunc(tied, func(cj *conjunct) bool {
		return cj.branch == nil || !sameText(admissionReaderValue.c, tied[0].branch.schema, cj.branch.schema)
	})
}

// principalKeywords are the keywords a node's build reads from the principal
// alone and no other conjunct folds into, so two conjuncts admitting alike
// still part on them: what each lowers its elements or its content by.
var principalKeywords = slices.Concat([]string{"items", "prefixItems", "additionalItems"}, contentKeywords)

// untieWords returns which of tied, conjuncts admitting alike that name their
// kinds by words, the position lowers from. The pick shows only in
// principalKeywords: of those writing every such keyword any other does, and
// alike, one is elected, and where none does, none is, and beside, the
// keywords beside the allOf, decide. Where nothing tells them apart, beside
// stands if tied, else the least text (leastText).
func untieWords(admissionReaderValue *admissionReader, beside *conjunct, tied []*conjunct) *conjunct {
	most := slices.MaxFunc(tied, func(a, b *conjunct) int {
		return countKeys(a.schema, principalKeywords) - countKeys(b.schema, principalKeywords)
	})
	if slices.ContainsFunc(tied, func(cj *conjunct) bool { return !writesWithin(cj.schema, most.schema) }) {
		return beside
	}
	tied = slices.DeleteFunc(slices.Clone(tied), func(cj *conjunct) bool { return !writesWithin(most.schema, cj.schema) })
	if slices.Contains(tied, beside) {
		return beside
	}
	return leastText(admissionReaderValue, tied)
}

// countKeys counts the keys of keys schema writes (hasKey).
func countKeys(schema *statement.Schema, keys []string) int {
	n := 0
	for _, key := range keys {
		if hasKey(schema, key) {
			n++
		}
	}
	return n
}

// writesWithin reports whether each of principalKeywords a writes, b writes
// alike.
func writesWithin(a, b *statement.Schema) bool {
	return !slices.ContainsFunc(principalKeywords, func(key string) bool {
		return hasKey(a, key) && (!hasKey(b, key) || !foldValue(a, key).agrees(foldValue(b, key)))
	})
}

// leastText returns the one of tied, allOf branches admitting alike, whose
// text is the least, bytewise: the pick shows in little more than where its
// node stands, and the text, its keys sorted, reads no order the branches are
// written in. Of a schema written twice, the first copy stands for both, the
// one place order shows, and only in that node's pointer.
func leastText(admissionReaderValue *admissionReader, tied []*conjunct) *conjunct {
	best := tied[0]
	for _, cj := range tied[1:] {
		if bytes.Compare(writtenText(admissionReaderValue.c, cj.branch.schema), writtenText(admissionReaderValue.c, best.branch.schema)) < 0 {
			best = cj
		}
	}
	return best
}

// narrowestRef returns the one of refs lying within the most conjuncts, a tie
// going to the least reference text, then to the least text of the entry
// writing it: a $ref's name stays put under edits to what it names, so no
// order written decides. Of a $ref written twice alike, the first copy stands
// for both, and keepsAtOutcome keeps neither.
func narrowestRef(admissionReaderValue *admissionReader, conjuncts []conjunct, refs []*conjunct) *conjunct {
	best, most := refs[0], admissionReaderValue.conesOver(conjuncts, refs[0].read)
	for _, ref := range refs[1:] {
		n := admissionReaderValue.conesOver(conjuncts, ref.read)
		if n > most || n == most && refBefore(admissionReaderValue, ref, best) {
			best, most = ref, n
		}
	}
	return best
}

// refBefore reports whether the $ref conjunct a goes before b where both lie
// within as many conjuncts: by reference text, then by the text of the entry.
func refBefore(admissionReaderValue *admissionReader, a, b *conjunct) bool {
	if order := strings.Compare(refText(a), refText(b)); order != 0 {
		return order < 0
	}
	return bytes.Compare(writtenText(admissionReaderValue.c, a.branch.schema), writtenText(admissionReaderValue.c, b.branch.schema)) < 0
}

// refText is the reference the $ref conjunct cj writes, as written.
func refText(cj *conjunct) string {
	_, text := refSpelling(cj.branch.schema)
	return text
}

// narrowsAll reports whether conjunct i is a value set within every other
// conjunct and more than a restatement of what a $ref of refs fixes, which a
// $ref's own is not. The keywords beside the allOf are one where they write an
// enum or a const (allOfDecides). No set is compared with itself, which answers
// nothing and walks the whole set.
func (w *admissionReader) narrowsAll(conjuncts []conjunct, i int, refs []*conjunct) bool {
	cj := &conjuncts[i]
	if !cj.read.fixed || slices.Contains(refs, cj) ||
		slices.ContainsFunc(refs, func(ref *conjunct) bool { return w.setWithin(ref.read, cj.read) }) {
		return false
	}
	for j := range conjuncts {
		if conjunct := &conjuncts[j]; j != i && (!cj.read.kindsWithin(conjunct.read) || !w.setWithin(cj.read, conjunct.read)) {
			return false
		}
	}
	return true
}

// setWithin is a.valuesWithin(b), telling walked how many members it looked up.
func (w *admissionReader) setWithin(a, b admitted) bool {
	within, walked := a.walkWithin(b)
	if w.walked != nil {
		w.walked(walked)
	}
	return within
}

// conesOver counts the conjuncts what read admits lies within.
func (w *admissionReader) conesOver(conjuncts []conjunct, read admitted) int {
	n := 0
	for j := range conjuncts {
		if w.within(read, conjuncts[j].read) {
			n++
		}
	}
	return n
}
