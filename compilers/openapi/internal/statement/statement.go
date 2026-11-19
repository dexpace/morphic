// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"encoding/json/jsontext"
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/oas"
)

// MaxFlattenDepth bounds how many allOf lists a statement flattens one inside
// another: an inline entry more lists than this hold that writes an allOf of
// its own is listed Capped, its entries unlisted, as the lowering keeps it
// verbatim (docs/compilers/openapi.md#allof-classification).
const MaxFlattenDepth = 32

// indexAbove is the number of keys past which a schema answers Key from an
// index built when it is read, rather than by scanning its keys.
const indexAbove = 16

// Statement is what the schema at a position writes: its own schema object,
// and its conjuncts: first the reference that object writes beside other
// keys, as an entry of its own at the object's pointer, which Schema then
// leaves out, then the entries of its inline allOf in the order the lowering
// flattens them, each wrapper after its own entries.
type Statement struct {
	*Schema
	Entries []AllOfConjunct
}

// AllOfConjunct is one conjunct of a statement: the schema object written there, its
// pointer below the statement's schema, such as /allOf/0/allOf/1, empty for
// the reference the statement's own object writes, whether it is a wrapper
// whose own entries precede it, and whether it writes an allOf nested past
// MaxFlattenDepth, which is not listed. An object writing a reference beside
// other keys is two entries at one pointer: the reference alone, then the
// rest, which writes none (Schema.WrittenBeside).
type AllOfConjunct struct {
	*Schema
	Rel     jsontext.Pointer
	Wrapper bool
	Capped  bool
}

// Schema is one schema object as read: where (zero for a value the bindings
// do not know), its node, aliases followed, whether it states anything, and,
// for one that does, its keys, reference, allOf entries and slots' values.
// Keys are the node's, within nodeview.MergeDepthLimit; a typed value is the
// parser's model's where its keyword is one of them, and a value kept as a
// node is its key's. A slot is nil where neither holds a keyword of it.
// Whether the rows read a key is its Slot.
type Schema struct {
	At   bind.DocumentPointer
	Node *yaml.Node
	Keys []Key
	// Unknown lists the keys the parser's model holds no field for, read
	// from the model, so it names a key merged in past
	// nodeview.MergeDepthLimit, which Keys lack.
	Unknown []string
	// Extensions lists the x- keys the schema writes, read from the model as
	// Unknown is.
	Extensions []Extension
	Ref        *Reference
	// BesideRef is, for the rest of an object a position writes, a reference
	// beside other keys, that reference as a schema object writing it alone,
	// which the statement lists as its first entry; nil for a whole object
	// and for the rest of an allOf entry.
	BesideRef *Schema
	// WrittenBeside is, for the rest of an allOf entry writing a reference
	// beside other keys, that reference, which the composition lists as the
	// entry before the rest. No reader of conjuncts reads it: one of how the
	// keys are spelt does, as a draft ignoring the keys beside a $ref does
	// (docs/compilers/openapi.md#dialect-keywords), and so does a report naming
	// the rest as written beside a $ref rather than as an inline entry.
	WrittenBeside *Schema
	AllOf         []Subschema

	Kinds  *Kinds
	Values *Values
	Format *Format
	Number *Number
	Text   *Text
	Array  *Array
	Object *Object
	Choice *Choice
	Logic  *Logic
	Notes  *Notes

	// m is the parser's value the schema was read from, index each key's
	// place in Keys, built only past indexAbove keys, and of the statement
	// MemoizingSchemaReader.Of built over the schema, nil until something reads it.
	m     *model
	index map[string]int
	of    *Statement

	Truth SchemaValueForm
	// Truncated marks a schema whose `<<` merges run past
	// nodeview.MergeDepthLimit: Keys lack what the merges past it bring, and
	// so does every typed value (docs/parser-library.md#merge-keys).
	Truncated bool
}

// Key returns the key name of s and whether it is written and read: a null
// is written and left out unless the keyword's value is an instance, as
// `const`, `default` and `example` hold one (docs/compilers/openapi.md#null-keywords).
// A key s does not write is the zero Key.
func (s *Schema) Key(name string) (Key, bool) {
	i, _ := s.find(name)
	if i < 0 {
		return Key{}, false
	}
	k := s.Keys[i]
	return k, k.read()
}

// find returns the place of name in s's keys, -1 where s writes none, and how
// many keys it compared name with to find it.
func (s *Schema) find(name string) (place, compared int) {
	if s == nil {
		return -1, 0
	}
	if s.index != nil {
		if i, ok := s.index[name]; ok {
			return i, 1
		}
		return -1, 1
	}
	for i, k := range s.Keys {
		if k.Name == name {
			return i, i + 1
		}
	}
	return -1, len(s.Keys)
}

// Key is one key a schema object writes: its name, its value with its alias
// followed, the slot the rows it is read with place it in and whether they
// define it only later (oas.SchemaRules.Slot), whether its value is null, and
// whether the parser's model holds it. The census of unknown keywords reads
// Schema.Unknown and not this, as a key merged in past
// nodeview.MergeDepthLimit is no Key.
type Key struct {
	Name                    string
	Node                    *yaml.Node
	Slot                    oas.Slot
	DefinedOnlyInLaterMinor bool
	Null                    bool
	Modeled                 bool
}

// Defined reports whether the rows the schema is read with define the key, as
// they do not for an extension or a keyword of another dialect.
func (k Key) Defined() bool { return k.Slot != oas.SlotUnknown }

// read reports whether the key's value is read: any but a null, which is
// read only where the keyword holds an instance.
func (k Key) read() bool {
	return !k.Null || k.Name == "const" || k.Name == "default" || k.Name == "example"
}

// ReadBeside reports whether a key named name, holding value, written beside
// a reference under keyword, says something of the instance the schema
// applies to, which makes the object the conjunction of the two (MemoizingSchemaReader.Of):
// any key but keyword's own whose value is read
// (docs/compilers/openapi.md#null-keywords), and none that only names the
// schema, holds schemas a reference reaches or comments, which the lowering
// drops (docs/compilers/openapi.md#dialect-keywords).
func ReadBeside(name string, value *yaml.Node, keyword string) bool {
	return name != keyword && !slices.Contains(keysThatConstrainNoInstance, name) && Key{Name: name, Null: isNull(value)}.read()
}

// keysThatConstrainNoInstance are the keys that say nothing of the instance their schema
// applies to: a comment, an anchor, and the schemas a reference reaches.
var keysThatConstrainNoInstance = []string{"$comment", "$anchor", "$dynamicAnchor", "$recursiveAnchor", "$defs", "definitions"}

// Extension is one x- key an object writes and its value, in the order the
// parser's model holds them: in source order, a key written twice once.
type Extension struct {
	Name string
	Node *yaml.Node
}

// Subschema is a schema written under a keyword: the name of the property, pattern
// or dependency it is written for, or its index in a list. MemoizingSchemaReader.Subschema reads
// it.
type Subschema struct {
	Name  string
	Index int
	m     *model
}

// Reference is the $ref a schema writes, as the bindings read it: the keyword
// it is written under, "$dynamicRef" where they read one as the $ref it
// spells (typed.Bindings.RefKeyword), the $ref the parser's model holds, empty
// for such a $dynamicRef and for an empty $ref, and the bindings' entry for
// it, Bound false where they hold none.
type Reference struct {
	Keyword, Text string
	Entry         bind.ReferenceSite
	Bound         bool
}

// SchemaValueForm is what a schema value states: Stated, the zero value, for a schema
// object; Anything and Nothing for `true` and `false`; Unwritten for none, or
// a value written as no schema (docs/compilers/openapi.md#null-schema); and
// Unread for a mapping the parser read as a boolean
// (docs/parser-library.md#failure-beneath-itself). Only a Stated schema has
// keys.
type SchemaValueForm uint8

// The truths of a schema value.
const (
	Stated SchemaValueForm = iota
	Anything
	Nothing
	Unwritten
	Unread
)

// ReadsFalse reports whether t reads as the false schema, which admits no
// value: Nothing, and Unread, a mapping the parser decodes as `false`
// (docs/parser-library.md#failure-beneath-itself), and not Unwritten, a value
// the parser decodes so though it is written as no schema
// (docs/parser-library.md#null-schema-is-false).
func (t SchemaValueForm) ReadsFalse() bool {
	return t == Nothing || t == Unread
}
