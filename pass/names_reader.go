// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"hash/maphash"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// nameReader reads one scope after another through the same buffers, which
// grow to the largest scope it reads, and keeps what each reports. A scope
// with nothing to report makes nothing of its own.
type nameReader struct {
	// parties are the names of the scope being read, in its order.
	parties []party
	// seen maps the hash of a name the scope compares to the first party of
	// the last set read whose name has that hash. A hash is its key rather
	// than the name, since a name compared joined is not a string the scope
	// holds.
	seen reusedMap[uint64, int]
	// hash makes the keys of seen, under one seed for the whole run, and
	// joined holds the words it hashes with their separators left out.
	hash   maphash.Hash
	joined []byte
	// later holds, for each party, the next party named alike, or -1 where
	// none is.
	later []int
	// last holds, for the first party of each set, the set's last party so
	// far, and other the first party of the set read before it whose name has
	// the same hash, or -1.
	last, other []int
	// shared holds the first party of each set of two or more named alike, in
	// the order their second parties were read.
	shared []int
	// bare holds the parties whose name has no word character.
	bare  []int
	diags []ir.Diagnostic
}

// comparison is how a scope's check tells two names with words apart, as the
// targets render the scope (docs/ir/naming.md#names-one-scope-shares).
type comparison uint8

const (
	// byWords compares the neutral words as they are.
	byWords comparison = iota
	// byJoinedWords compares the neutral words joined with no separator, so
	// set_up meets setup, for a scope a first target renders as a package's
	// name or a class file's.
	byJoinedWords
)

// party is what a scope's check reads of one entity's name: the neutral words
// a target renders it from, Canonical or else Hint, and the spelling Source
// keeps.
type party struct {
	words, source string
}

// partyOf reads n as a party.
func partyOf(n ir.Naming) party {
	words := n.Canonical
	if words == "" {
		words = n.Hint
	}
	return party{words: words, source: n.Source}
}

// scope says how to report on the parties a nameReader read: what holds them,
// what they are, and how to name each.
type scope struct {
	// in is what holds the scope, such as "operation group", and id its ID;
	// in is empty for a scope the document holds.
	in, id string
	// one names a party, and many several, such as "operation" and
	// "operations".
	one, many string
	// ids name the parties, in scope order: each by its ID, and a union's
	// variant, which has none, by the ID of the type it holds. It is nil for a
	// model's properties and an enum's members, named by index, as their IDs
	// hold a source key or value verbatim, control characters included. Any
	// other ID can carry one from a name too, written raw where diagnostics
	// render (GitHub #816).
	ids []string
}

// at is where every finding of sc is reported: what holds the scope, whose ID
// no declaration order moves, unlike a party's place in it. The message names
// the parties.
func (sc scope) at() string {
	if sc.in == "" {
		return ir.DocumentPath
	}
	return sc.id
}

// idsOf lists n IDs, the ith read through id, for a scope's report, and only
// for one.
func idsOf(n int, id func(i int) string) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = id(i)
	}
	return ids
}

// label names party i within sc.
func (sc scope) label(i int) string {
	if sc.ids == nil {
		return strconv.Itoa(i)
	}
	return sc.ids[i]
}

// lead writes what holds sc, as a message about it begins, or nothing for a
// scope the document holds.
func (sc scope) lead(b *strings.Builder) {
	if sc.in == "" {
		return
	}
	fmt.Fprintf(b, "in %s %s, ", sc.in, sc.id)
}

// readScope reads n names, the ith through name, as one scope whose names
// compare under by, and reports whether it found anything to report. A name is
// read by index, never through an element's address or a generic over the
// service tree, which servicetreewrites refuses outside ir and the compilers.
func (r *nameReader) readScope(by comparison, n int, name func(i int) ir.Naming) bool {
	r.parties = r.parties[:0]
	for i := range n {
		r.parties = append(r.parties, partyOf(name(i)))
	}
	return r.read(by)
}

// read finds, among r.parties, each set of two or more whose names compare
// alike under by and the parties whose name has no word character, and reports
// whether it found either. A party with neither words nor a source names
// nothing, which irverify reports as ir/naming-absent, so it is passed over
// here.
func (r *nameReader) read(by comparison) bool {
	seen, n := r.seen.emptied(), len(r.parties)
	r.later, r.last, r.other = sized(r.later, n), sized(r.last, n), sized(r.other, n)
	r.shared, r.bare = r.shared[:0], r.bare[:0]
	for i, p := range r.parties {
		r.later[i] = -1
		if p.words == "" {
			if p.source == "" {
				continue
			}
			r.bare = append(r.bare, i)
		}
		r.place(by, seen, r.sum(by, p), i)
	}
	return len(r.shared) > 0 || len(r.bare) > 0
}

// sized returns s holding n elements, reusing its array where it can.
func sized(s []int, n int) []int {
	return slices.Grow(s[:0], n)[:n]
}

// sum hashes the name by compares p by: its words, joined where by joins them,
// or else its spelling.
func (r *nameReader) sum(by comparison, p party) uint64 {
	r.hash.Reset()
	switch {
	case p.words == "":
		r.hash.WriteString(p.source)
	case by == byJoinedWords:
		r.joined = r.joined[:0]
		for i := range len(p.words) {
			if p.words[i] != '_' {
				r.joined = append(r.joined, p.words[i])
			}
		}
		r.hash.Write(r.joined)
	default:
		r.hash.WriteString(p.words)
	}
	return r.hash.Sum64()
}

// place puts party i, whose name hashes to sum, in the set of the parties
// before it named alike under by, or starts a set of its own where none is.
func (r *nameReader) place(by comparison, seen map[uint64]int, sum uint64, i int) {
	head, ok := seen[sum]
	if !ok {
		head = -1
	}
	if first := r.setOf(by, head, i); first >= 0 {
		if r.last[first] == first {
			r.shared = append(r.shared, first)
		}
		r.later[r.last[first]] = i
		r.last[first] = i
		return
	}
	r.last[i], r.other[i] = i, head
	seen[sum] = i
}

// setOf returns the first party of the set whose name party i's compares
// alike to under by, among the sets from head on that share its name's hash,
// or -1 where none does. A set past the first is that of a name that differs
// though its hash agrees.
func (r *nameReader) setOf(by comparison, head, i int) int {
	for first := head; first >= 0; first = r.other[first] {
		if alike(by, r.parties[first], r.parties[i]) {
			return first
		}
	}
	return -1
}

// alike reports whether by compares the names of a and b alike. A name with no
// word character is compared by its spelling alone, which every target names
// alike and no neutral name equals, as each holds a letter or digit.
func alike(by comparison, a, b party) bool {
	switch {
	case a.words == "" || b.words == "":
		return a.words == b.words && a.source == b.source
	case by == byJoinedWords:
		return joinedEqual(a.words, b.words)
	default:
		return a.words == b.words
	}
}

// joinedEqual reports whether a and b are equal with every underscore
// removed, without making either. Each step drops one byte of each, and an
// underscore is one byte that no other rune's encoding holds.
func joinedEqual(a, b string) bool {
	a, b = strings.TrimLeft(a, "_"), strings.TrimLeft(b, "_")
	for a != "" && b != "" && a[0] == b[0] {
		a, b = strings.TrimLeft(a[1:], "_"), strings.TrimLeft(b[1:], "_")
	}
	return a == "" && b == ""
}

// report appends what read found in sc, at what holds it: for each set of
// parties named alike a pass/name-collision warning naming every party in it,
// and for each party with no word character a pass/name-without-words info
// naming that party.
func (r *nameReader) report(sc scope) {
	for _, first := range r.shared {
		r.diags = append(r.diags, diag(ir.SeverityWarning, "pass/name-collision", r.collision(sc, first), sc.at()))
	}
	for _, i := range r.bare {
		var b strings.Builder
		sc.lead(&b)
		fmt.Fprintf(&b, "%s %s (%q) has no word character, so it has no neutral name, and a target names it "+
			"from its characters", sc.one, sc.label(i), r.parties[i].source)
		r.diags = append(r.diags, diag(ir.SeverityInfo, "pass/name-without-words", b.String(), sc.at()))
	}
}

// collision is the message for the parties named alike from first, in scope
// order, each named with the spelling its source gave it, and then the neutral
// name they share or, in a scope that compares names joined, the neutral names
// that differ only where their words break.
func (r *nameReader) collision(sc scope, first int) string {
	var b strings.Builder
	sc.lead(&b)
	b.WriteString(sc.many)
	b.WriteByte(' ')
	for i := first; i >= 0; i = r.later[i] {
		b.WriteString(separator(i == first, r.later[i] < 0))
		b.WriteString(sc.label(i))
		if src := r.parties[i].source; src != "" {
			fmt.Fprintf(&b, " (%q)", src)
		}
	}
	who := "a target"
	switch words := r.wordsOf(first); len(words) {
	case 0:
		fmt.Fprintf(&b, " share the spelling %q, which has no word character", r.parties[first].source)
	case 1:
		fmt.Fprintf(&b, " share the neutral name %q", words[0])
	default:
		b.WriteString(" have the neutral names ")
		for k, w := range words {
			b.WriteString(separator(k == 0, k == len(words)-1))
			fmt.Fprintf(&b, "%q", w)
		}
		b.WriteString(", which differ only where their words break")
		who = "a target that joins words or ignores letter case"
	}
	fmt.Fprintf(&b, ", so %s can tell them apart only by ID", who)
	return b.String()
}

// wordsOf lists the neutral words of the parties named alike from first, each
// once, in scope order: none where their names have no word character.
func (r *nameReader) wordsOf(first int) []string {
	var words []string
	for i := first; i >= 0; i = r.later[i] {
		if w := r.parties[i].words; w != "" && !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	return words
}

// separator is what precedes an item of an English list: nothing before the
// first, "and" before the last, and a comma before any other.
func separator(first, last bool) string {
	switch {
	case first:
		return ""
	case last:
		return " and "
	default:
		return ", "
	}
}
