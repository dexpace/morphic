// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"encoding/json/jsontext"
	"strconv"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// Source is what a Table reads: the bindings holding each schema of the
// documents and where it was read, and the rows each position's schema is
// read with.
type Source struct {
	Bindings *typed.Bindings
	Dialects Dialects
}

// Dialects answers the Schema Object's rows a schema read at a position is
// read with: the rows of the dialect in force there.
type Dialects interface {
	At(p bind.Position) oas.SchemaRules
}

// Table reads schema objects into statements and remembers each, keyed by the
// parser's value, which is one per coordinate (docs/parser-library.md#alias-expansion).
// What it remembers turns on the documents and their bindings alone, so every
// walk of a run shares one. It is not safe for concurrent use.
type Table struct {
	src        Source
	schemas    map[*model]*Schema
	statements map[*model]*Statement
	// reads counts the schema objects read, which the tests hold to one each.
	reads int
}

// NewTable returns a table reading from src, which has read nothing yet. A
// nil Dialects reads every key with the zero rows, which define none.
func NewTable(src Source) *Table {
	return &Table{src: src, schemas: map[*model]*Schema{}, statements: map[*model]*Statement{}}
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
// an entry as a position of its own. A nil s reads as Unwritten.
func (t *Table) Read(s *Schema) *Statement {
	if s == nil {
		return t.Of(nil)
	}
	return t.Of(s.m)
}

// rules returns the rows the schema read at p is read with.
func (t *Table) rules(p bind.Position) oas.SchemaRules {
	if t.src.Dialects == nil {
		return oas.SchemaRules{}
	}
	return t.src.Dialects.At(p)
}

// readKeys places each key s's node writes, in the order the parser holds
// them (nodeview.Pairs), in the slot rules give it, and marks those unknown
// lists, the keys the parser's model holds no field for. Past indexAbove keys
// it indexes them first, so that marking, and every later Key, finds a key
// without scanning.
func readKeys(s *Schema, rules oas.SchemaRules, unknown []string) {
	pairs := nodeview.Pairs(s.Node)
	if len(pairs) == 0 {
		return
	}
	s.Keys = make([]Key, len(pairs))
	for i, p := range pairs {
		slot, later := rules.Slot(p.Key)
		s.Keys[i] = Key{Name: p.Key, Node: p.Val, Slot: slot, Later: later, Null: isNull(p.Val), Modeled: true}
	}
	if len(s.Keys) > indexAbove {
		s.index = make(map[string]int, len(s.Keys))
		for i, k := range s.Keys {
			s.index[k.Name] = i
		}
	}
	for _, name := range unknown {
		if i, _ := s.find(name); i >= 0 {
			s.Keys[i].Modeled = false
		}
	}
}

// flatten lists the inline allOf entries of st's schema as the lowering
// flattens them: in source order, depth first, an entry that names no
// reference and writes an allOf of its own replaced by its entries and then
// listed itself as a Wrapper, while at most MaxFlattenDepth lists hold it,
// and listed Capped past that. It walks an explicit stack of at most
// MaxFlattenDepth+1 lists, reading each entry's schema object and never an
// entry's statement, so it adds no recursion.
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
		wraps := entry.Ref == nil && len(entry.AllOf) > 0
		if wraps && len(stack) <= MaxFlattenDepth {
			stack = append(stack, frame{list: entry.AllOf, owner: entry, rel: rel})
			continue
		}
		st.Entries = append(st.Entries, Entry{Schema: entry, Rel: rel, Capped: wraps})
	}
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
