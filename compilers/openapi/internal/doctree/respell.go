// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
)

// lineBreaks are the characters YAML 1.1 ends a line at and YAML 1.2 reads as
// any other: U+0085, U+2028 and U+2029.
var lineBreaks = [3]string{"\u0085", " ", " "}

// escapedSlash is the escape YAML 1.2 allows and the parser refuses.
const escapedSlash = `\/`

// firstPlaceholder is the lowest placeholder tried, and placeholders how many
// are tried: the private-use code points from it, one bit each of a uint64.
const (
	firstPlaceholder = 0xE000
	placeholders     = 64
)

// respelling is what respell put in place of each trigger it respelled, for
// restore to read back.
type respelling struct {
	breaks [3]rune // what stands for U+0085, U+2028 and U+2029, or 0
	slash  rune    // what, written twice, stands for an escaped slash, or 0
}

// respell returns text as the parser reads it as YAML 1.2, one character for
// one (docs/parser-library.md#yaml11-lexing), what it put in place of each
// trigger is returned as it is, and so is one opening with a UTF-16 or UTF-32
// byte order mark, which Read hands it only when it encodes no text
// (transcoded).
//
// Each line-break character, and each escaped slash, is written as a
// placeholder: a private-use character no spelling of the text can produce
// (spelled), so restore reads back exactly what was put. A trigger left with no
// placeholder free is left as written.
func respell(text []byte) ([]byte, respelling, []Directive) {
	if wide(text) {
		return text, respelling{}, nil
	}
	dirs, out := directives(text)
	var present [3]bool
	for i, b := range lineBreaks {
		present[i] = bytes.Contains(text, []byte(b))
	}
	slashed := bytes.Contains(text, []byte(escapedSlash))
	if present == [3]bool{} && !slashed {
		return out, respelling{}, dirs
	}

	var r respelling
	free := ^spelled(text)
	for i, b := range lineBreaks {
		if present[i] {
			r.breaks[i] = take(&free)
			out = replaceBreak(out, b, r.breaks[i])
		}
	}
	if slashed {
		r.slash = take(&free)
		out = replaceSlashes(out, r.slash)
	}
	return out, r, dirs
}

// wide reports whether text opens with a UTF-16 or UTF-32 byte order mark.
func wide(text []byte) bool {
	_, _, ok := encodingOf(text)
	return ok
}

// encodingOf returns the width in bytes of a code unit of the encoding text's
// byte order mark names, UTF-32's before UTF-16's whose mark opens it, and
// whether it is big-endian; false for a text opening with no such mark.
func encodingOf(text []byte) (width int, bigEndian, ok bool) {
	switch {
	case bytes.HasPrefix(text, []byte("\x00\x00\xfe\xff")):
		return 4, true, true
	case bytes.HasPrefix(text, []byte("\xff\xfe\x00\x00")):
		return 4, false, true
	case bytes.HasPrefix(text, []byte("\xfe\xff")):
		return 2, true, true
	case bytes.HasPrefix(text, []byte("\xff\xfe")):
		return 2, false, true
	default:
		return 0, false, false
	}
}

// transcoded returns text, a UTF-16 or UTF-32 text opening with its byte order
// mark, as the UTF-8 it encodes, without the mark, which YAML reads as no
// character, and true; false for any other text, or one holding a unit that
// encodes no character, half a surrogate pair or a truncated unit, which is
// left for the parser to refuse. YAML 1.2 reads all three encodings alike.
func transcoded(text []byte) ([]byte, bool) {
	width, bigEndian, ok := encodingOf(text)
	if !ok || len(text)%width != 0 {
		return nil, false
	}
	units := make([]rune, 0, len(text)/width)
	for i := width; i < len(text); i += width {
		units = append(units, unitAt(text[i:i+width], bigEndian))
	}
	if width == 2 {
		units = utf16Runes(units)
	}
	out := make([]byte, 0, len(units))
	for _, r := range units {
		if !utf8.ValidRune(r) {
			return nil, false
		}
		out = utf8.AppendRune(out, r)
	}
	return out, true
}

// unitAt reads the code unit unit holds, in the byte order bigEndian names.
func unitAt(unit []byte, bigEndian bool) rune {
	var v rune
	for i := range unit {
		b := unit[i]
		if !bigEndian {
			b = unit[len(unit)-1-i]
		}
		v = v<<8 | rune(b)
	}
	return v
}

// utf16Runes joins each surrogate pair of units into the character it encodes,
// and leaves half a pair as itself, which is no valid rune.
func utf16Runes(units []rune) []rune {
	out := units[:0]
	for i := 0; i < len(units); i++ {
		if r := utf16.DecodeRune(units[i], pairOf(units, i)); r != utf8.RuneError {
			out = append(out, r)
			i++
			continue
		}
		out = append(out, units[i])
	}
	return out
}

// pairOf is the unit after units[i], or none past the last.
func pairOf(units []rune, i int) rune {
	if i+1 < len(units) {
		return units[i+1]
	}
	return 0
}

// take returns the lowest placeholder free holds and removes it, or 0 when it
// holds none.
func take(free *uint64) rune {
	for i := range placeholders {
		if *free&(1<<i) != 0 {
			*free &^= 1 << i
			return rune(firstPlaceholder + i)
		}
	}
	return 0
}

// replaceBreak returns text with each brk written as p, or text itself when p
// is 0.
func replaceBreak(text []byte, brk string, p rune) []byte {
	if p == 0 {
		return text
	}
	return bytes.ReplaceAll(text, []byte(brk), []byte(string(p)))
}

// replaceSlashes returns text with each slash that follows a run of
// backslashes of odd length, and that run's last backslash, written as p
// twice: in a double-quoted scalar the last backslash of an odd run escapes
// what follows it, and of an even run is itself escaped. It returns text itself
// when p is 0.
func replaceSlashes(text []byte, p rune) []byte {
	if p == 0 {
		return text
	}
	pair := []byte(string([]rune{p, p}))
	out := make([]byte, 0, len(text)+len(text)/8)
	run, last := 0, 0
	for i, b := range text {
		switch {
		case b == '\\':
			run++
			continue
		case b == '/' && run%2 == 1:
			out = append(append(out, text[last:i-1]...), pair...)
			last = i + 1
		}
		run = 0
	}
	return append(out, text[last:]...)
}

// spelled returns the placeholders text can make a string of the tree hold, a
// bit each from firstPlaceholder: one written raw, one a \u or \U escape names
// in either case and of either length, and one in the UTF-8 decoding of a run
// of %HH escapes, which the parser decodes in a tag. Nothing else makes a
// private-use character: a named escape names a fixed one outside the range,
// and the parser joins no surrogates. It errs only toward spelled.
func spelled(text []byte) uint64 {
	var m uint64
	for i := range text {
		switch text[i] {
		case 0xEE:
			m |= mark(placeholderAt(text, i, 1))
		case '\\':
			m |= mark(escapeAt(text, i, 4)) | mark(escapeAt(text, i, 8))
		case '%':
			m |= mark(percentAt(text, i))
		}
	}
	return m
}

// mark is the bit of r among the placeholders, or none for another rune.
func mark(r rune) uint64 {
	if r < firstPlaceholder || r >= firstPlaceholder+placeholders {
		return 0
	}
	return 1 << (r - firstPlaceholder)
}

// placeholderAt returns the character whose UTF-8 encoding starts at text[i],
// every byte step bytes apart, or utf8.RuneError.
func placeholderAt(text []byte, i, step int) rune {
	if i+2*step >= len(text) {
		return utf8.RuneError
	}
	r, _ := utf8.DecodeRune([]byte{text[i], text[i+step], text[i+2*step]})
	return r
}

// escapeAt returns the code point a backslash at text[i], a u or a U, and n
// hexadecimal digits name, or utf8.RuneError.
func escapeAt(text []byte, i, n int) rune {
	if i+2+n > len(text) || (text[i+1] != 'u' && text[i+1] != 'U') {
		return utf8.RuneError
	}
	v, err := strconv.ParseUint(string(text[i+2:i+2+n]), 16, 32)
	if err != nil {
		return utf8.RuneError
	}
	return rune(v)
}

// percentAt returns the character three %HH escapes from text[i] encode, or
// utf8.RuneError.
func percentAt(text []byte, i int) rune {
	if i+9 > len(text) || text[i+3] != '%' || text[i+6] != '%' {
		return utf8.RuneError
	}
	var enc [3]byte
	for k := range enc {
		v, err := strconv.ParseUint(string(text[i+3*k+1:i+3*k+3]), 16, 8)
		if err != nil {
			return utf8.RuneError
		}
		enc[k] = byte(v)
	}
	return placeholderAt(enc[:], 0, 1)
}

// restore writes back in n what respell put in place: each line-break
// placeholder as its character, in every string of n, and an escaped slash's
// doubled placeholder as the slash it escapes in the value of a double-quoted
// scalar, the one style where the backslash was an escape, and as the two
// characters written in every other string.
func (r respelling) restore(n *yaml.Node) {
	if r == (respelling{}) {
		return
	}
	quoted := n.Kind == yaml.ScalarNode && n.Style&yaml.DoubleQuotedStyle != 0
	n.Value = r.restored(n.Value, quoted)
	n.Tag = r.restored(n.Tag, false)
	n.Anchor = r.restored(n.Anchor, false)
	n.HeadComment = r.restored(n.HeadComment, false)
	n.LineComment = r.restored(n.LineComment, false)
	n.FootComment = r.restored(n.FootComment, false)
}

// restored is s with what respell put in place written back, the escaped slash
// as a slash when escapes reads it as an escape.
func (r respelling) restored(s string, escapes bool) string {
	for i, p := range r.breaks {
		if p != 0 {
			s = strings.ReplaceAll(s, string(p), lineBreaks[i])
		}
	}
	if r.slash == 0 {
		return s
	}
	slash := escapedSlash
	if escapes {
		slash = "/"
	}
	return strings.ReplaceAll(s, string([]rune{r.slash, r.slash}), slash)
}

// breakIn returns the line-break character whose placeholder s holds, the first
// of them, or 0: a character the text writes raw, which YAML 1.1 ends a line
// at.
func (r respelling) breakIn(s string) rune {
	for i, p := range r.breaks {
		if p != 0 && strings.ContainsRune(s, p) {
			c, _ := utf8.DecodeRuneInString(lineBreaks[i])
			return c
		}
	}
	return 0
}

// directives returns text with each %YAML directive declaring a minor past 1
// written as 1.1, padded with spaces to its width, which the parser reads as it
// reads any document (docs/parser-library.md#yaml11-lexing), and every %YAML
// 1.x directive the stream writes, in order. A line counts as a directive in
// the stream's prologue, where only blank lines, comments and directives stand
// above it, and after a document end marker. A major other than 1 is left for
// the parser to refuse, as YAML 1.2 asks.
func directives(text []byte) ([]Directive, []byte) {
	if !bytes.Contains(text, []byte("%YAML")) {
		return nil, text
	}
	var dirs []Directive
	out := text
	prologue := true
	for line, start := 1, 0; start < len(text); line++ {
		end, next := lineAt(text, start)
		l := text[start:end]
		if line == 1 {
			l = bytes.TrimPrefix(l, []byte(bom))
		}
		switch {
		case prologue && bytes.HasPrefix(l, []byte("%")):
			if d, at, ok := yamlDirective(l, line); ok {
				dirs = append(dirs, d)
				out = rewriteVersion(out, text, end-len(l)+at.start, end-len(l)+at.end, d.Minor)
			}
		case prologue && blankOrComment(l):
		case prologue:
			prologue = false
		case documentEnd(l):
			prologue = true
		}
		start = next
	}
	return dirs, out
}

// lineAt returns where the line beginning at start ends, before its break, and
// where the next line begins. A line ends at LF, at CR, or at CR LF taken as
// one.
func lineAt(text []byte, start int) (end, next int) {
	i := bytes.IndexAny(text[start:], "\r\n")
	if i < 0 {
		return len(text), len(text)
	}
	end = start + i
	if text[end] == '\r' && end+1 < len(text) && text[end+1] == '\n' {
		return end, end + 2
	}
	return end, end + 1
}

// span is where a token lies in its line.
type span struct{ start, end int }

// yamlDirective reads l as a `%YAML 1.<minor>` directive written on line, and
// returns it with where its version lies in l. A directive the parser would
// refuse, or one naming another major, is no directive here.
func yamlDirective(l []byte, line int) (Directive, span, bool) {
	rest, ok := bytes.CutPrefix(l, []byte("%YAML"))
	if !ok || len(rest) == 0 || !isBlank(rest[0]) {
		return Directive{}, span{}, false
	}
	start := len(l) - len(bytes.TrimLeft(rest, " \t"))
	end := start + bytes.IndexAny(append(l[start:len(l):len(l)], ' '), " \t")
	minor, ok := bytes.CutPrefix(l[start:end], []byte("1."))
	if !ok || !blankOrComment(l[end:]) {
		return Directive{}, span{}, false
	}
	n, err := strconv.Atoi(string(minor))
	if err != nil || minor[0] == '+' || minor[0] == '-' {
		return Directive{}, span{}, false
	}
	return Directive{Minor: n, Line: line}, span{start, end}, true
}

// rewriteVersion returns out with the version text holds from start to end
// written as 1.1 and padded to its width, when minor is past 1, copying text
// the first time it writes.
func rewriteVersion(out, text []byte, start, end, minor int) []byte {
	if minor < 2 {
		return out
	}
	if &out[0] == &text[0] {
		out = bytes.Clone(text)
	}
	copy(out[start:end], "1.1"+strings.Repeat(" ", end-start-len("1.1")))
	return out
}

// blankOrComment reports whether l holds only spaces and tabs, then a comment
// or nothing.
func blankOrComment(l []byte) bool {
	rest := bytes.TrimLeft(l, " \t")
	return len(rest) == 0 || (rest[0] == '#' && (len(rest) == len(l) || isBlank(l[len(l)-len(rest)-1])))
}

// documentEnd reports whether l is a document end marker, `...` alone or before
// blanks and a comment.
func documentEnd(l []byte) bool {
	rest, ok := bytes.CutPrefix(l, []byte("..."))
	return ok && (len(rest) == 0 || isBlank(rest[0])) && blankOrComment(rest)
}

// isBlank reports whether b is a space or a tab.
func isBlank(b byte) bool { return b == ' ' || b == '\t' }
