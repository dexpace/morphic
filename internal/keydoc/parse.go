package keydoc

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Entry is one entry of a keyed document.
type Entry struct {
	// Key is the slug the entry's heading names.
	Key string
	// Line is the line the heading is on, counting from 1.
	Line int
	// Text is the entry's prose: every line it writes outside fenced code and
	// its fields, joined by newlines.
	Text string
	// Fields are the fields the entry writes, in order.
	Fields []Field
}

// Field returns the field e writes under name, and whether it writes one.
func (e Entry) Field(name string) (Field, bool) {
	for _, f := range e.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Field is one field of an entry: a bullet at the margin naming it, written
// "- Name: value" or "- Name:", and the indented lines it runs on over.
type Field struct {
	// Name is what the bullet writes before its colon.
	Name string
	// Line is the line the bullet is on.
	Line int
	// Value is what the bullet writes after its colon, trimmed.
	Value string
	// Lines are the indented lines below the bullet, as written.
	Lines []string
}

// Text returns the field's value and each line it runs on over, trimmed and
// joined by single spaces: the field as one line of prose.
func (f Field) Text() string {
	parts := append([]string{f.Value}, f.Lines...)
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

var (
	// keyHeading is an entry's heading: its key, a lowercase slug, in code.
	keyHeading = regexp.MustCompile("^### `([a-z0-9]+(?:-[a-z0-9]+)*)`$")
	// heading is any Markdown heading.
	heading = regexp.MustCompile(`^#{1,6} `)
	// fieldBullet is a field's bullet: a capitalized name and a colon.
	fieldBullet = regexp.MustCompile(`^- ([A-Z][A-Za-z ]*):(?: (.*))?$`)
	// codeSpan is one span of code.
	codeSpan = regexp.MustCompile("`([^`]+)`")
)

// Spans returns the text of each span of code s writes, in order.
func Spans(s string) []string {
	matches := codeSpan.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// Parse reads the entries of a keyed document whose fields are named fields,
// and returns each problem with its shape as a message naming its line: a
// third-level heading that names no key, a bullet at an entry's margin that
// names no field, a field fields does not list, and a field written twice. An
// entry ends at the next heading of any level, and fenced code is not read.
func Parse(text string, fields ...string) ([]Entry, []string) {
	p := reader{known: fields}
	for i, line := range strings.Split(text, "\n") {
		p.read(i+1, line)
	}
	p.close()
	return p.entries, p.problems
}

// reader is Parse's state between lines.
type reader struct {
	known    []string
	entries  []Entry
	problems []string
	// cur is the entry being read, and text its prose so far.
	cur  *Entry
	text []string
	// field is the field whose indented lines are being read: one of cur's,
	// or a discarded one for a bullet that was refused.
	field  *Field
	fenced bool
}

// read takes one line of the document.
func (p *reader) read(n int, line string) {
	if p.field != nil && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "" {
		p.field.Lines = append(p.field.Lines, line)
		return
	}
	p.field = nil
	if strings.HasPrefix(line, "```") {
		p.fenced = !p.fenced
		return
	}
	switch {
	case p.fenced:
	case strings.HasPrefix(line, "### "):
		p.open(n, line)
	case heading.MatchString(line):
		p.close()
	case p.cur == nil:
	case strings.HasPrefix(line, "- "):
		p.bullet(n, line)
	default:
		p.text = append(p.text, line)
	}
}

// open starts the entry a third-level heading names, or reports a heading
// that names none.
func (p *reader) open(n int, line string) {
	p.close()
	m := keyHeading.FindStringSubmatch(line)
	if m == nil {
		p.problems = append(p.problems, fmt.Sprintf("line %d: a third-level heading is not a key: %q", n, line))
		return
	}
	p.entries = append(p.entries, Entry{Key: m[1], Line: n})
	p.cur = &p.entries[len(p.entries)-1]
}

// close ends the entry being read, if there is one.
func (p *reader) close() {
	if p.cur != nil {
		p.cur.Text = strings.Trim(strings.Join(p.text, "\n"), "\n")
	}
	p.cur, p.text = nil, nil
}

// bullet reads a bullet at the margin of the entry being read as a field.
func (p *reader) bullet(n int, line string) {
	refuse := func(format string, args ...any) {
		p.problems = append(p.problems, fmt.Sprintf("line %d: %s ", n, p.cur.Key)+fmt.Sprintf(format, args...))
		p.field = &Field{}
	}
	m := fieldBullet.FindStringSubmatch(line)
	switch {
	case m == nil:
		refuse("writes a bullet that names no field: %q", line)
	case !slices.Contains(p.known, m[1]):
		refuse("writes a field no reader knows: %q", line)
	case slices.ContainsFunc(p.cur.Fields, func(f Field) bool { return f.Name == m[1] }):
		refuse("writes the field %q twice", m[1])
	default:
		p.cur.Fields = append(p.cur.Fields, Field{Name: m[1], Line: n, Value: strings.TrimSpace(m[2])})
		p.field = &p.cur.Fields[len(p.cur.Fields)-1]
	}
}
