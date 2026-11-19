// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// Source is what a Table reads: the bindings holding each schema of the
// documents and where it was read, and the rows each position's schema is
// read with, those of the dialect in force there (dialect.Table.At).
type Source struct {
	Bindings *typed.Bindings
	Rules    dialect.Table
}

// Table reads schema objects into statements and remembers each, keyed by the
// parser's value, which is one per coordinate (docs/parser-library.md#alias-expansion).
// What it remembers turns on the documents and their bindings alone, so every
// walk of a run shares one. It is not safe for concurrent use.
type Table struct {
	src        Source
	schemas    map[*model]*Schema
	statements map[*model]*Statement
	// rests and references hold the two conjuncts of each schema object
	// writing a reference beside keys it reads (conjoins), each built once:
	// the keys as an allOf entry reads them, and the reference.
	rests, references map[*Schema]*Schema
	// reads counts the schema objects read, which the tests hold to one each,
	// and lookups the statements Of looks up, which they hold Read to none of
	// for a schema Of has built the statement of.
	reads, lookups int
}

// NewTable returns a table reading from src, which has read nothing yet. The
// zero Rules reads every key with the zero rows, which define none.
func NewTable(src Source) *Table {
	return &Table{
		src: src, schemas: map[*model]*Schema{}, statements: map[*model]*Statement{},
		rests: map[*Schema]*Schema{}, references: map[*Schema]*Schema{},
	}
}

// At returns the statement of the schema the bindings hold at p, an Unwritten
// one where they hold none.
func (t *Table) At(p bind.Position) *Statement {
	return t.Of(t.src.Bindings.At(p))
}

// Sub returns the statement of the schema s names.
func (t *Table) Sub(s Sub) *Statement {
	return t.Of(s.m)
}

// Read returns the statement of the schema object s, for a caller lowering
// an entry as a position of its own: the one Of built over s, which s keeps,
// so that a schema read once more costs no lookup, or, for the rest of an
// allOf entry (restOf), one listing its own allOf and no reference. A nil s
// reads as Unwritten, and so does one no table read, which holds no value. s
// is one t read: a schema another table built a statement over keeps that
// statement, whichever table is asked.
func (t *Table) Read(s *Schema) *Statement {
	switch {
	case s == nil:
		return t.Of(nil)
	case s.of != nil:
		return s.of
	case s.WrittenBeside != nil:
		st := &Statement{Schema: s}
		s.of = st
		t.flatten(st)
		return st
	}
	return t.Of(s.m)
}

// readKeys places each key s's node writes, in the order the parser holds
// them (nodeview.View.MappingPairs), in the slot rules give it, marks those
// s.Unknown lists, the keys the parser's model holds no field for, and marks s
// Truncated where its merges run past nodeview.MergeDepthLimit. Past
// indexAbove keys it indexes them first, so that marking, and every later Key,
// finds a key without scanning.
func readKeys(s *Schema, rules oas.SchemaRules) {
	view := nodeview.New()
	pairs := view.MappingPairs(s.Node)
	s.Truncated = !view.Expanded(s.Node)
	if len(pairs) == 0 {
		return
	}
	s.Keys = make([]Key, len(pairs))
	for i, p := range pairs {
		slot, later := rules.Slot(p.Key)
		s.Keys[i] = Key{Name: p.Key, Node: p.Val, Slot: slot, Later: later, Null: isNull(p.Val), Modeled: true}
	}
	index(s)
	for _, name := range s.Unknown {
		if i, _ := s.find(name); i >= 0 {
			s.Keys[i].Modeled = false
		}
	}
}

// index maps each key of s to its place, past indexAbove keys, and leaves s
// none at or below it.
func index(s *Schema) {
	s.index = nil
	if len(s.Keys) <= indexAbove {
		return
	}
	s.index = make(map[string]int, len(s.Keys))
	for i, k := range s.Keys {
		s.index[k.Name] = i
	}
}

// flatten lists the inline allOf entries of st's schema as the lowering
// flattens them: in source order, depth first, an entry writing a reference
// beside what else it says (conjoins) listed as that reference and then the
// rest (restOf), an entry that writes an allOf and no reference, whatever else
// it writes, replaced by its entries and then listed itself as a Wrapper, while at
// most MaxFlattenDepth lists hold it, and listed Capped past that. It walks an
// explicit stack of at most MaxFlattenDepth+1 lists, reading each entry's
// schema object and never an entry's statement, so it adds no recursion.
func (t *Table) flatten(st *Statement) {
	type frame struct {
		list  []Sub
		next  int
		owner *Schema
		rel   jsontext.Pointer
	}
	stack := []frame{{list: st.AllOf}}
	for len(stack) > 0 {
		f := &stack[len(stack)-1]
		if f.next == len(f.list) {
			owner, rel := f.owner, f.rel
			stack = stack[:len(stack)-1]
			if owner != nil {
				st.Entries = append(st.Entries, Entry{Schema: owner, Rel: rel, Wrapper: true})
			}
			continue
		}
		entry := t.read(f.list[f.next].m)
		rel := f.rel + "/allOf/" + jsontext.Pointer(strconv.Itoa(f.next))
		f.next++
		if conjoins(entry) {
			st.Entries = append(st.Entries, Entry{Schema: t.reference(entry), Rel: rel, Split: true})
			entry = t.restOf(entry)
		}
		wraps := entry.Ref == nil && len(entry.AllOf) > 0
		if wraps && len(stack) <= MaxFlattenDepth {
			stack = append(stack, frame{list: entry.AllOf, owner: entry, rel: rel})
			continue
		}
		st.Entries = append(st.Entries, Entry{Schema: entry, Rel: rel, Capped: wraps})
	}
}

// conjoins reports whether s writes a reference and a key beside it that it
// reads. JSON Schema applies the keywords beside a $ref to the instance with
// it (Core 2020-12 §8.2.3.1), so s says what an allOf of the two says: the
// reference is one conjunct and the rest another
// (docs/compilers/openapi.md#ref-use-site). A reference merged in past
// nodeview.MergeDepthLimit, which no key of s names, conjoins as any does.
func conjoins(s *Schema) bool {
	if s.Truth != Stated || s.Ref == nil || failedEntry(s) {
		return false
	}
	return slices.ContainsFunc(s.Keys, func(k Key) bool { return ReadBeside(k.Name, k.Node, s.Ref.Keyword) })
}

// failedEntry reports whether s, writing a reference, is an allOf entry whose
// reference the bindings read as ending on no schema. Such an entry is read
// whole: nothing reads what it writes beside the reference, which the
// composition keeps as the entry wrote it
// (docs/compilers/openapi.md#allof-inline-residue). Written anywhere else, the
// same object conjoins, as `{allOf: [{$ref: …}], …}` does.
func failedEntry(s *Schema) bool {
	if !s.Ref.Bound || s.Ref.Entry.Chain.Status == bind.Resolved {
		return false
	}
	at := string(s.At.Pointer)
	i := strings.LastIndexByte(at, '/')
	if i < 0 || !strings.HasSuffix(at[:i], "/allOf") {
		return false
	}
	// An entry's last token is its index: an items, a not or a property
	// under a property named allOf is no entry. The schema package reads an
	// entry off a pointer too, by a parse of its own that takes no signed
	// index where this one does (writtenAsEntry). Neither knows how the
	// schema is reached; a statement saying so replaces both (GitHub #2058).
	_, err := strconv.Atoi(at[i+1:])
	return err == nil
}

// refKey returns the place in s, which writes a reference, of the key naming
// it, and false where none does: the model holds a $ref merged in past
// nodeview.MergeDepthLimit, which s's keys do not list (Truncated).
func refKey(s *Schema) (int, bool) {
	i, _ := s.find(s.Ref.Keyword)
	return i, i >= 0
}

// own returns the schema object js holds as its statement reads it: the
// object itself, or, for one writing a reference beside other keys
// (conjoins), the rest of it, whose reference is the statement's first entry.
func (t *Table) own(js *model) *Schema {
	s := t.read(js)
	if conjoins(s) {
		return t.wordsOf(s)
	}
	return s
}

// wordsOf returns s, which conjoins and a position writes, less its
// reference (lessReference), and the reference as its BesideRef: the
// position's statement lists the two. Of builds it once.
func (t *Table) wordsOf(s *Schema) *Schema {
	w := lessReference(s)
	w.BesideRef = t.reference(s)
	return w
}

// restOf returns s, an allOf entry that conjoins, less its reference
// (lessReference): the conjunct its composition lists after that reference,
// as `{allOf: [{$ref: …}, {…}]}` lists it. It writes no reference, its node
// holding its keys alone, so no reader of the composition's conjuncts reads
// the reference twice; it names it WrittenBeside, for a reader of how its
// keys are spelt (docs/compilers/openapi.md#ref-use-site).
func (t *Table) restOf(s *Schema) *Schema {
	if r, ok := t.rests[s]; ok {
		return r
	}
	r := lessReference(s)
	r.WrittenBeside = t.reference(s)
	content := make([]*yaml.Node, 0, 2*len(r.Keys))
	for _, k := range r.Keys {
		content = append(content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k.Name}, k.Node)
	}
	r.Node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: content}
	t.rests[s] = r
	return r
}

// lessReference returns a copy of s, which conjoins, holding every key but the
// one writing its reference, and every slot it fills. It shares s's values.
func lessReference(s *Schema) *Schema {
	w := *s
	w.Ref, w.of = nil, nil
	w.Keys = slices.DeleteFunc(slices.Clone(s.Keys), func(k Key) bool { return k.Name == s.Ref.Keyword })
	index(&w)
	return &w
}

// reference returns the reference s, which conjoins, writes, as a schema
// object writing it alone, its node too, and its statement, which lists no
// entry. A reference no key of s names is read from its text.
func (t *Table) reference(s *Schema) *Schema {
	if r, ok := t.references[s]; ok {
		return r
	}
	r := &Schema{At: s.At, Ref: s.Ref, m: s.m}
	value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s.Ref.Text}
	if i, named := refKey(s); named {
		r.Keys, value = []Key{s.Keys[i]}, s.Keys[i].Node
	}
	// Its node writes the reference alone, so it reads as the entry
	// `{$ref: …}` would, not as the object it is written in.
	r.Node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: s.Ref.Keyword}, value,
	}}
	r.of = &Statement{Schema: r}
	t.references[s] = r
	return r
}

// open makes s's struct of slot, where it has none yet: a key of the slot's
// keywords opens it, whether or not its value is read.
func open(s *Schema, slot oas.Slot) {
	switch {
	case slot == oas.SlotKinds && s.Kinds == nil:
		s.Kinds = &Kinds{}
	case slot == oas.SlotValues && s.Values == nil:
		s.Values = &Values{}
	case slot == oas.SlotFormat && s.Format == nil:
		s.Format = &Format{}
	case slot == oas.SlotNumber && s.Number == nil:
		s.Number = &Number{}
	case slot == oas.SlotText && s.Text == nil:
		s.Text = &Text{}
	case slot == oas.SlotArray && s.Array == nil:
		s.Array = &Array{}
	case slot == oas.SlotObject && s.Object == nil:
		s.Object = &Object{}
	case slot == oas.SlotChoice && s.Choice == nil:
		s.Choice = &Choice{}
	case slot == oas.SlotLogic && s.Logic == nil:
		s.Logic = &Logic{}
	case slot == oas.SlotNotes && s.Notes == nil:
		s.Notes = &Notes{}
	}
}

// isNull reports whether n, its alias followed, is a YAML null.
func isNull(n *yaml.Node) bool {
	n = nodeview.Deref(n)
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}
