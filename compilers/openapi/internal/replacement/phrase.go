// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// named is a name a message gives an entity, and where it is written: the
// bytes text[start:end] of the message.
type named struct {
	text       string
	start, end int
}

// The words a phrase is read by, in lower case
// (docs/compilers/openapi.md#replacement-inference).
var (
	// leads may stand before a `use` opening its sentence.
	leads = map[string]bool{"please": true, "should": true, "must": true, "instead": true}
	// cues introduce a replacement anywhere in a sentence, by their last word
	// and the words before it.
	cues = map[string]bool{
		"renamed to": true, "replaced by": true, "replaced with": true, "superseded by": true,
		"in favor of": true, "in favour of": true,
	}
	// negations before a cue in its sentence rule it out, beside a word ending
	// in n't.
	negations = map[string]bool{"not": true, "never": true, "no": true, "cannot": true}
	// fillers may stand between a cue and the name it introduces.
	fillers = map[string]bool{"the": true, "a": true, "an": true, "new": true}
)

// phraseState is where a reading stands between a cue and the names after it.
type phraseState int

const (
	// outside is no phrase: a name met here is not introduced.
	outside phraseState = iota
	// expecting follows a cue and any filler: the next name is introduced.
	expecting
	// introduced follows a name a phrase introduced.
	introduced
	// joined follows an introduced name and one joining word: the next name is
	// introduced too.
	joined
)

// phrase is one forward reading of a message: the state of its sentence, and
// the names its phrases introduce so far.
type phrase struct {
	text  string
	state phraseState
	// negated reports a negation earlier in the sentence.
	negated bool
	// lead reports that nothing but words stands before here in the sentence,
	// a comma after a lead word aside, so a `use` here may open it.
	lead bool
	// last is the sentence's last word, and window the words before it, both
	// reset where anything but a word intervenes.
	last, window string
	out          []string
}

// introducedNames returns the text of each of names, the names text gives,
// in order and none overlapping, that a phrase introduces as a replacement:
// a cue, no negation before it in its sentence, at most fillers, then a name
// and each joined to it by a comma, `and` or `or` alone. It reads text once,
// forward.
func introducedNames(text string, names []named) []string {
	r := phrase{text: text}
	r.sentence()
	at := 0
	for _, n := range names {
		r.gap(at, n.start)
		r.name(n)
		at = n.end
	}
	r.gap(at, len(text))
	return r.out
}

// sentence starts a sentence.
func (r *phrase) sentence() {
	r.state, r.negated, r.lead, r.last, r.window = outside, false, true, "", ""
}

// gap reads text[from:to], which holds no name, token by token.
func (r *phrase) gap(from, to int) {
	for i := from; i < to; {
		c, size := utf8.DecodeRuneInString(r.text[i:])
		switch {
		case isWordStart(c):
			end := wordEnd(r.text, i, to)
			r.word(strings.ToLower(r.text[i:end]))
			i = end
			continue
		case c == '\n' && blankLineFollows(r.text[i+size:to]):
			r.sentence()
		case unicode.IsSpace(c):
		case strings.ContainsRune(".!?;", c) && r.endsHere(i+size):
			r.sentence()
		case c == ',':
			r.comma()
		case strings.ContainsRune("(\"'“‘", c) && r.state == expecting:
		default:
			r.other()
		}
		i += size
	}
}

// endsHere reports whether a mark ending at i ends a sentence: white space or
// the end of the message follows it.
func (r *phrase) endsHere(i int) bool {
	if i == len(r.text) {
		return true
	}
	c, _ := utf8.DecodeRuneInString(r.text[i:])
	return unicode.IsSpace(c)
}

// word reads one word, in lower case.
func (r *phrase) word(w string) {
	switch {
	case r.state == expecting && fillers[w]:
		r.remember(w)
		return
	case r.state == introduced && (w == "and" || w == "or"):
		r.state = joined
		return
	}
	r.state = outside
	r.negated = r.negated || negations[w] || strings.HasSuffix(w, "n't") || strings.HasSuffix(w, "n’t")
	cue := (w == "use" && r.lead && (r.last == "" || leads[r.last])) ||
		cues[r.last+" "+w] || cues[r.window+" "+r.last+" "+w]
	r.remember(w)
	if cue && !r.negated {
		r.state = expecting
	}
}

// remember makes w the sentence's last word.
func (r *phrase) remember(w string) {
	r.window, r.last = r.last, w
}

// comma reads a comma: a joining word after an introduced name, else
// something between words that a lead word alone may stand before.
func (r *phrase) comma() {
	if r.state == introduced {
		r.state = joined
		return
	}
	lead := r.lead && leads[r.last]
	r.other()
	r.lead = lead
}

// other reads any token but a word, a comma or a sentence's end.
func (r *phrase) other() {
	r.state, r.lead, r.last, r.window = outside, false, "", ""
}

// name reads a name: introduced where a cue or a joining word leads to it.
func (r *phrase) name(n named) {
	if r.state == expecting || r.state == joined {
		r.out = append(r.out, n.text)
		r.state = introduced
	} else {
		r.state = outside
	}
	r.lead, r.last, r.window = false, "", ""
}

// isWordStart reports whether c begins a word: a letter or a digit.
func isWordStart(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsDigit(c)
}

// wordEnd returns where the word starting at text[i] ends, before to: letters,
// digits, apostrophes, hyphens and underscores.
func wordEnd(text string, i, to int) int {
	for i < to {
		c, size := utf8.DecodeRuneInString(text[i:])
		if !isWordStart(c) && !strings.ContainsRune("'’-_", c) {
			break
		}
		i += size
	}
	return i
}

// blankLineFollows reports whether rest, what follows a line ending, is a
// blank line: white space other than a line ending, then a line ending.
func blankLineFollows(rest string) bool {
	trimmed := strings.TrimLeft(rest, " \t\r")
	return strings.HasPrefix(trimmed, "\n")
}
