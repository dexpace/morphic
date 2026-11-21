// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package jsonpath

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxPatternBytes bounds the I-Regexp pattern match and search read; a longer
// one matches nothing, as a pattern that is no I-Regexp does (RFC 9535 section
// 2.4.6). Go's regexp engine runs in time linear in its input, never
// backtracking, so the bound is on the pattern's own size.
const maxPatternBytes = 4096

// maxProgram bounds the instructions a pattern's program may hold, as the
// translator counts them. Go's engine runs in time linear in its input times
// its program, and a repetition makes a program far larger than its text, so
// a pattern past the bound matches nothing, as one that is no I-Regexp does.
const maxProgram = 1 << 14

// maxRepeat is the most a repetition may count, which Go's engine holds by
// refusing more. A count is read no further than one past it, so the size of
// the program is reckoned with no overflow, and the engine then refuses it.
const maxRepeat = 1000

// translator rewrites an I-Regexp (RFC 9485) as the Go regexp that matches
// the same strings, checking the pattern by its grammar as it goes, in one
// pass with no recursion: a group is counted, not descended into. It counts
// the instructions the program will hold, a frame per open group.
type translator struct {
	out     strings.Builder
	src     string
	frames  []frame
	anchors anchors
	pos     int
	// atom reports that a quantifier may follow: the last thing read is an
	// atom with no quantifier yet.
	atom bool
	// caret and dollar report a `^` and a `$` read outside a class, unescaped,
	// at any depth, which the translation writes as anchors.
	caret, dollar bool
}

// translation is a pattern rewritten for Go's engine: its source, the
// instructions its program holds, why no string matches it, "" where one may,
// and what its anchors read as (readAsAnchors), "" where it writes none.
type translation struct {
	source, empty, anchored string
	size                    int
}

// Why no string matches a branch in which an anchor stands where Go's engine
// reads it, and the class a writer meaning the character writes instead.
const (
	endAnchored   = "$ anchors the end of the string, and a character must follow it; write [$] to match the character"
	startAnchored = `^ anchors the start of the string, and a character must precede it; write [\^] to match the character`
)

// readAsAnchors says what a pattern writing `^`, `$` or both outside a class
// reads them as, and the class that writes each character instead, or ""
// where it writes neither.
func readAsAnchors(caret, dollar bool) string {
	const grammar = "where RFC 9485's grammar reads "
	switch {
	case caret && dollar:
		return `^ and $ are read as anchors, ` + grammar + `the characters; write [\^] and [$] to match them`
	case caret:
		return `^ is read as an anchor, ` + grammar + `the character; write [\^] to match it`
	case dollar:
		return "$ is read as an anchor, " + grammar + "the character; write [$] to match it"
	default:
		return ""
	}
}

// anchors reads the top level of a pattern for a branch no string matches:
// one in which `$` comes before a character it must consume, or `^` after
// one. A group is read as a character it need not consume, and a pattern
// matches nothing only where each of its top-level branches does, so what it
// reports holds.
type anchors struct {
	// none is why a branch read matches nothing, "" before one did; alive
	// reports a branch read that may match.
	none  string
	alive bool
	// why is why the branch being read matches nothing, "" while it may.
	why string
	// must reports that the last atom read must consume a character unless
	// a quantifier admitting none follows it; consumed, that one before it
	// must; ended, that `$` was read.
	must, consumed, ended bool
}

// read takes the top-level token r, before it is translated.
func (a *anchors) read(r rune) {
	switch r {
	case '*', '?':
		a.must = false
		return
	case '+', '{':
		return // a range admitting none is read by rangeQuantifier
	}
	a.commit()
	switch r {
	case '^':
		if a.consumed && a.why == "" {
			a.why = startAnchored
		}
	case '$':
		a.ended = true
	case '|':
		a.settle()
	case '(':
	default:
		a.must = true // a character, an escape, a class or a dot
	}
}

// commit settles the last atom: a character the branch must consume.
func (a *anchors) commit() {
	if !a.must {
		return
	}
	if a.ended && a.why == "" {
		a.why = endAnchored
	}
	a.consumed, a.must = true, false
}

// settle ends a top-level branch and starts the next.
func (a *anchors) settle() {
	a.commit()
	if a.why == "" {
		a.alive = true
	} else if a.none == "" {
		a.none = a.why
	}
	*a = anchors{none: a.none, alive: a.alive}
}

// frame counts the instructions of one group: its finished branches, the
// branch being read, and the last atom of it, which a quantifier repeats.
type frame struct{ done, branch, last int }

// top returns the frame of the innermost open group.
func (t *translator) top() *frame { return &t.frames[len(t.frames)-1] }

// atomOf counts an atom of size instructions, which a quantifier may follow,
// and reports whether the program is still within maxProgram.
func (t *translator) atomOf(size int) bool {
	frame := t.top()
	frame.branch += size
	frame.last = size
	t.atom = true
	return frame.branch <= maxProgram
}

// repeat counts the last atom repeated copies times, with a split each, and
// reports whether the program is still within maxProgram. A *, + or ? holds
// one copy and one split.
func (t *translator) repeat(copies int) bool {
	frame := t.top()
	frame.branch += frame.last*(copies-1) + copies
	return frame.branch <= maxProgram
}

// translate returns what pattern reads as in Go's syntax, and false for a
// pattern that is no I-Regexp, is past maxPatternBytes, or compiles past
// maxProgram.
func translate(pattern string) (translation, bool) {
	if len(pattern) > maxPatternBytes || !utf8.ValidString(pattern) {
		return translation{}, false
	}
	translatorValue := &translator{src: pattern, frames: []frame{{}}}
	for translatorValue.pos < len(translatorValue.src) {
		if !translatorValue.next() {
			return translation{}, false
		}
	}
	if len(translatorValue.frames) != 1 {
		return translation{}, false
	}
	translatorValue.anchors.settle()
	tr := translation{source: translatorValue.out.String(), size: translatorValue.top().done + translatorValue.top().branch,
		anchored: readAsAnchors(translatorValue.caret, translatorValue.dollar)}
	if !translatorValue.anchors.alive {
		tr.empty = translatorValue.anchors.none
	}
	return tr, true
}

// take returns the next character of the pattern and moves past it.
func (t *translator) take() rune {
	r, size := utf8.DecodeRuneInString(t.src[t.pos:])
	t.pos += size
	return r
}

// next reads one token of the pattern outside a character class.
func (t *translator) next() bool {
	r := t.take()
	if len(t.frames) == 1 {
		t.anchors.read(r)
	}
	switch r {
	case '(':
		t.frames = append(t.frames, frame{})
		t.out.WriteString("(?:")
		t.atom = false
	case ')':
		return t.closeGroup()
	case '|':
		frame := t.top()
		frame.done, frame.branch, frame.last = frame.done+frame.branch+1, 0, 0
		t.out.WriteByte('|')
		t.atom = false
	case '*', '+', '?':
		return t.quantify(string(r), 1)
	case '{':
		return t.rangeQuantifier()
	case '.':
		t.out.WriteString(`[^\n\r]`)
		return t.atomOf(1)
	case '[':
		return t.class()
	case '\\':
		return t.escapeOutside()
	case ']', '}':
		return false
	case '^', '$':
		// RFC 9485 section 5.4 maps a pattern to RE2, Go's dialect, leaving
		// these as written, where they anchor; the compliance suite reads
		// them so (docs/compilers/openapi.md#overlays).
		t.caret, t.dollar = t.caret || r == '^', t.dollar || r == '$'
		t.out.WriteRune(r)
		return t.atomOf(1)
	default:
		t.out.WriteString(regexp.QuoteMeta(string(r)))
		return t.atomOf(1)
	}
	return true
}

// closeGroup ends the innermost group, which counts as an atom of the one
// holding it.
func (t *translator) closeGroup() bool {
	if len(t.frames) == 1 {
		return false
	}
	frame := t.top()
	size := frame.done + frame.branch + 1
	t.frames = t.frames[:len(t.frames)-1]
	t.out.WriteByte(')')
	return t.atomOf(size)
}

// quantify writes a quantifier, which only an atom takes, once, counting the
// atom repeated copies times.
func (t *translator) quantify(q string, copies int) bool {
	if !t.atom {
		return false
	}
	t.out.WriteString(q)
	t.atom = false
	return t.repeat(copies)
}

// rangeQuantifier reads {n}, {n,} or {n,m} after its brace.
func (t *translator) rangeQuantifier() bool {
	start := t.pos
	copies, read := t.count()
	if !read {
		return false
	}
	low := copies
	if t.pos < len(t.src) && t.src[t.pos] == ',' {
		t.pos++
		high, bounded := t.count()
		copies = max(high, copies+1)
		if bounded {
			copies = high
		}
	}
	if t.pos >= len(t.src) || t.src[t.pos] != '}' {
		return false
	}
	if low == 0 && len(t.frames) == 1 {
		t.anchors.must = false // a range from zero admits none
	}
	t.pos++
	return t.quantify("{"+t.src[start:t.pos], max(copies, 1))
}

// count reads a run of ASCII digits as a count of at most maxRepeat+1, and
// reports whether there was one.
func (t *translator) count() (int, bool) {
	start, n := t.pos, 0
	for t.pos < len(t.src) && t.src[t.pos] >= '0' && t.src[t.pos] <= '9' {
		n = min(n*10+int(t.src[t.pos]-'0'), maxRepeat+1)
		t.pos++
	}
	return n, t.pos > start
}

// escapeOutside reads an escape outside a class: one character, or a
// category, which is written as a class of its own.
func (t *translator) escapeOutside() bool {
	items, char, ok := t.escape()
	switch {
	case !ok:
		return false
	case items != "":
		t.out.WriteString("[" + items + "]")
	default:
		t.out.WriteString(class(char))
	}
	return t.atomOf(1)
}

// escape reads an escape after its backslash: a single character escape,
// returned as its character, or a category escape, returned as the Go class
// items it stands for.
func (t *translator) escape() (items string, char rune, ok bool) {
	if t.pos >= len(t.src) {
		return "", 0, false
	}
	switch r := t.take(); {
	case r == 'p' || r == 'P':
		items, ok = t.category(r == 'P')
		return items, 0, ok
	case r == 'n':
		return "", '\n', true
	case r == 'r':
		return "", '\r', true
	case r == 't':
		return "", '\t', true
	case strings.ContainsRune(`()*+-.?[\]^{|}`, r):
		return "", r, true
	default:
		return "", 0, false
	}
}

// class writes r as an item of a Go character class.
func class(r rune) string { return fmt.Sprintf(`\x{%x}`, r) }

// categoryNames are the category names I-Regexp admits after \p or \P, each
// between spaces.
const categoryNames = " L Ll Lm Lo Lt Lu M Mc Me Mn N Nd Nl No P Pc Pd Pe Pf Pi Po Ps " +
	"Z Zl Zp Zs S Sc Sk Sm So C Cc Cf Cn Co "

// category reads {name} after \p or \P and returns the Go class item that
// matches what it names, complemented for \P. Go's tables name every category
// I-Regexp does, Cn among them, and read C as Unicode does, with Cn in it.
func (t *translator) category(complement bool) (string, bool) {
	end := strings.IndexByte(t.src[t.pos:], '}')
	if t.pos >= len(t.src) || t.src[t.pos] != '{' || end < 0 {
		return "", false
	}
	name := t.src[t.pos+1 : t.pos+end]
	t.pos += end + 1
	if name == "" || !strings.Contains(categoryNames, " "+name+" ") {
		return "", false
	}
	if complement {
		return `\P{` + name + `}`, true
	}
	return `\p{` + name + `}`, true
}

// classItem reads one character of a class, a character or a single
// character escape, and reports whether it is one; a category is returned as
// its items.
func (t *translator) classItem(r rune) (items string, char rune, ok bool) {
	switch r {
	case '\\':
		return t.escape()
	case '[', ']', '-':
		return "", 0, false
	default:
		return "", r, true
	}
}

// class reads a character class after its bracket: an optional ^, an optional
// leading -, then characters, ranges and category escapes, then an optional
// trailing -.
func (t *translator) class() bool {
	var items strings.Builder
	negated := t.pos < len(t.src) && t.src[t.pos] == '^'
	if negated {
		t.pos++
	}
	if t.pos < len(t.src) && t.src[t.pos] == '-' {
		t.pos++
		items.WriteString(class('-'))
	}
	for t.pos < len(t.src) {
		r := t.take()
		switch {
		case r == ']' && items.Len() > 0:
			return t.writeClass(negated, items.String())
		case r == '-' && t.pos < len(t.src) && t.src[t.pos] == ']' && items.Len() > 0:
			items.WriteString(class('-'))
			continue
		}
		item, ok := t.rangeItem(r)
		if !ok {
			return false
		}
		items.WriteString(item)
	}
	return false
}

// rangeItem reads one item of a class starting with r: a category, a
// character, or a range of two characters in order.
func (t *translator) rangeItem(r rune) (string, bool) {
	cat, low, ok := t.classItem(r)
	if !ok || cat != "" {
		return cat, ok
	}
	if t.pos+1 >= len(t.src) || t.src[t.pos] != '-' || t.src[t.pos+1] == ']' {
		return class(low), true
	}
	t.pos++
	cat, high, ok := t.classItem(t.take())
	if !ok || cat != "" || high < low {
		return "", false
	}
	return class(low) + "-" + class(high), true
}

// writeClass writes a class of items, one instruction of the program.
func (t *translator) writeClass(negated bool, items string) bool {
	t.out.WriteByte('[')
	if negated {
		t.out.WriteByte('^')
	}
	t.out.WriteString(items)
	t.out.WriteByte(']')
	return t.atomOf(1)
}
