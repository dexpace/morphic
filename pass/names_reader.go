// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package pass

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// nameReader reads one scope after another through the same buffers, which
// grow to the largest scope it reads, and keeps what each reports. A scope
// with nothing to report makes nothing of its own.
type nameReader struct {
	// parties are the names of the scope being read, in its order.
	parties []party
	// seen maps a name the scope compares to the parties so far that have it.
	seen reusedMap[string, chain]
	// later holds, for each party, the next party sharing its neutral name, or
	// -1 where none does.
	later []int
	// shared holds the first party of each name two or more share, in the
	// order their second parties were read.
	shared []int
	// bare holds the parties whose name has no word character.
	bare  []int
	diags []ir.Diagnostic
}

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

// chain is the first and the last of the parties read so far that share a
// neutral name.
type chain struct {
	first, last int
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
	// ids name the parties, in scope order: each by its own ID, and a union's
	// variant, which has none, by the ID of the type it holds. An index would
	// name a place in the IR's list, which need not be one the source wrote.
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

// lead writes what holds sc, as a message about it begins, or nothing for a
// scope the document holds.
func (sc scope) lead(b *strings.Builder) {
	if sc.in == "" {
		return
	}
	fmt.Fprintf(b, "in %s %s, ", sc.in, sc.id)
}

// readScope reads n names, the ith through name, as one scope, and reports
// whether it found anything to report. A name is read by index, never through
// an element's address or a generic over the service tree, which
// servicetreewrites refuses outside ir and the compilers.
func (r *nameReader) readScope(n int, name func(i int) ir.Naming) bool {
	r.parties = r.parties[:0]
	for i := range n {
		r.parties = append(r.parties, partyOf(name(i)))
	}
	return r.read()
}

// read finds, among r.parties, the names two or more parties share and the
// parties whose name has no word character, and reports whether it found
// either. A name with no word character is compared by its spelling, which
// every target names alike and no neutral name equals, as each holds a letter
// or digit. A party with neither words nor a source names nothing, which
// irverify reports as ir/naming-absent, so it is passed over here.
func (r *nameReader) read() bool {
	seen := r.seen.emptied()
	r.later = slices.Grow(r.later[:0], len(r.parties))[:len(r.parties)]
	r.shared, r.bare = r.shared[:0], r.bare[:0]
	for i, p := range r.parties {
		r.later[i] = -1
		key := p.words
		if key == "" {
			if p.source == "" {
				continue
			}
			r.bare = append(r.bare, i)
			key = p.source
		}
		c, ok := seen[key]
		if !ok {
			seen[key] = chain{first: i, last: i}
			continue
		}
		if c.first == c.last {
			r.shared = append(r.shared, c.first)
		}
		r.later[c.last] = i
		seen[key] = chain{first: c.first, last: i}
	}
	return len(r.shared) > 0 || len(r.bare) > 0
}

// report appends what read found in sc, at what holds it: for each shared
// name a pass/name-collision warning naming every party that has it,
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
			"from its characters", sc.one, sc.ids[i], r.parties[i].source)
		r.diags = append(r.diags, diag(ir.SeverityInfo, "pass/name-without-words", b.String(), sc.at()))
	}
}

// collision is the message for the parties sharing first's name, in scope
// order, each named with the spelling its source gave it.
func (r *nameReader) collision(sc scope, first int) string {
	var b strings.Builder
	sc.lead(&b)
	b.WriteString(sc.many)
	for i := first; i >= 0; i = r.later[i] {
		switch {
		case i == first:
			b.WriteByte(' ')
		case r.later[i] < 0:
			b.WriteString(" and ")
		default:
			b.WriteString(", ")
		}
		b.WriteString(sc.ids[i])
		if src := r.parties[i].source; src != "" {
			fmt.Fprintf(&b, " (%q)", src)
		}
	}
	if words := r.parties[first].words; words != "" {
		fmt.Fprintf(&b, " share the neutral name %q", words)
	} else {
		fmt.Fprintf(&b, " share the spelling %q, which has no word character", r.parties[first].source)
	}
	b.WriteString(", so a target can tell them apart only by ID")
	return b.String()
}
