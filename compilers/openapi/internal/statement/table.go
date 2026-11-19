// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	"encoding/json/jsontext"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/dialect"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable"
	"github.com/dexpace/morphic/compilers/openapi/internal/reftable/typed"
	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// Source is what a Table reads: the bindings holding each schema of the
// documents and where it was read, and the rows each position's schema is read
// with, those of the dialect in force there (dialect.SchemaDialectRows.At).
type Source struct {
	Bindings *typed.Bindings
	Rules    dialect.SchemaDialectRows
}

// MemoizingSchemaReader reads schema objects into statements and remembers
// each, keyed by the parser's value, which is one per coordinate
// (docs/parser-library.md#alias-expansion). What it remembers turns on the
// documents and their bindings alone, so every walk of a run shares one. It is
// not safe for concurrent use.
type MemoizingSchemaReader struct {
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
func NewTable(src Source) *MemoizingSchemaReader {
	return &MemoizingSchemaReader{
		src: src, schemas: map[*model]*Schema{}, statements: map[*model]*Statement{},
		rests: map[*Schema]*Schema{}, references: map[*Schema]*Schema{},
	}
}

// At returns the statement of the schema the bindings hold at documentPointer,
// an Unwritten one where they hold none.
func (t *MemoizingSchemaReader) At(documentPointer reftable.DocumentPointer) *Statement {
	return t.Of(t.src.Bindings.At(documentPointer))
}

// Subschema returns the statement of the schema subschema names.
func (t *MemoizingSchemaReader) Subschema(subschema Subschema) *Statement {
	return t.Of(subschema.m)
}

// Read returns the statement of the schema object schema, for a caller lowering
// an entry as a position of its own: the one Of built over schema, which schema
// keeps, so that a schema read once more costs no lookup, or, for the rest of
// an allOf entry (restOf), one listing its own allOf and no reference. A nil
// schema reads as Unwritten, and so does one no table read, which holds no
// value. schema is one t read: a schema another table built a statement over
// keeps that statement, whichever table is asked.
func (t *MemoizingSchemaReader) Read(schema *Schema) *Statement {
	switch {
	case schema == nil:
		return t.Of(nil)
	case schema.of != nil:
		return schema.of
	case schema.WrittenBeside != nil:
		st := &Statement{Schema: schema}
		schema.of = st
		t.flatten(st)
		return st
	}
	return t.Of(schema.m)
}

// readKeys places each key schema's node writes, in the order the parser holds
// them (nodeview.View.MappingPairs), in the slot rules give it, marks those
// schema.Unknown lists, the keys the parser's model holds no field for, and
// marks schema Truncated where its merges run past nodeview.MergeDepthLimit.
// Past indexAbove keys it indexes them first, so that marking, and every later
// Key, finds a key without scanning.
func readKeys(schema *Schema, rules specrules.SchemaRules) {
	view := nodeview.New()
	pairs := view.MappingPairs(schema.Node)
	schema.Truncated = !view.Expanded(schema.Node)
	if len(pairs) == 0 {
		return
	}
	schema.Keys = make([]Key, len(pairs))
	for i, pair := range pairs {
		slot, later := rules.Slot(pair.Key)
		schema.Keys[i] = Key{Name: pair.Key, Node: pair.Val, Slot: slot, DefinedOnlyInLaterMinor: later, Null: isNull(pair.Val), Modeled: true}
	}
	index(schema)
	for _, name := range schema.Unknown {
		if i, _ := schema.find(name); i >= 0 {
			schema.Keys[i].Modeled = false
		}
	}
}

// index maps each key of schema to its place, past indexAbove keys, and leaves
// schema none at or below it.
func index(schema *Schema) {
	schema.index = nil
	if len(schema.Keys) <= indexAbove {
		return
	}
	schema.index = make(map[string]int, len(schema.Keys))
	for i, key := range schema.Keys {
		schema.index[key.Name] = i
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
func (t *MemoizingSchemaReader) flatten(st *Statement) {
	type frame struct {
		list  []Subschema
		next  int
		owner *Schema
		rel   jsontext.Pointer
	}
	stack := []frame{{list: st.AllOf}}
	for len(stack) > 0 {
		frameValue := &stack[len(stack)-1]
		if frameValue.next == len(frameValue.list) {
			owner, rel := frameValue.owner, frameValue.rel
			stack = stack[:len(stack)-1]
			if owner != nil {
				st.Entries = append(st.Entries, AllOfConjunct{Schema: owner, Rel: rel, Wrapper: true})
			}
			continue
		}
		entry := t.read(frameValue.list[frameValue.next].m)
		rel := frameValue.rel + "/allOf/" + jsontext.Pointer(strconv.Itoa(frameValue.next))
		frameValue.next++
		if conjoins(entry) {
			st.Entries = append(st.Entries, AllOfConjunct{Schema: t.reference(entry), Rel: rel})
			entry = t.restOf(entry)
		}
		wraps := entry.Ref == nil && len(entry.AllOf) > 0
		if wraps && len(stack) <= MaxFlattenDepth {
			stack = append(stack, frame{list: entry.AllOf, owner: entry, rel: rel})
			continue
		}
		st.Entries = append(st.Entries, AllOfConjunct{Schema: entry, Rel: rel, Capped: wraps})
	}
}

// conjoins reports whether schema writes a reference and a key beside it that
// it reads. JSON Schema applies the keywords beside a $ref to the instance with
// it (Core 2020-12 §8.2.3.1), so schema says what an allOf of the two says: the
// reference is one conjunct and the rest another
// (docs/compilers/openapi.md#ref-use-site). A reference merged in past
// nodeview.MergeDepthLimit, which no key of schema names, conjoins as any does.
func conjoins(schema *Schema) bool {
	if schema.Truth != Stated || schema.Ref == nil || failedEntry(schema) {
		return false
	}
	return slices.ContainsFunc(schema.Keys, func(key Key) bool { return ReadBeside(key.Name, key.Node, schema.Ref.Keyword) })
}

// failedEntry reports whether schema, writing a reference, is an allOf entry
// whose reference the bindings read as ending on no schema. Such an entry is
// read whole: nothing reads what it writes beside the reference, which the
// composition keeps as the entry wrote it
// (docs/compilers/openapi.md#allof-inline-residue). Written anywhere else, the
// same object conjoins, as `{allOf: [{$ref: …}], …}` does.
func failedEntry(schema *Schema) bool {
	if !schema.Ref.Bound || schema.Ref.Entry.Chain.Status == reftable.Resolved {
		return false
	}
	at := string(schema.At.Pointer)
	i := strings.LastIndexByte(at, '/')
	if i < 0 || !strings.HasSuffix(at[:i], "/allOf") {
		return false
	}
	// An entry's last token is its index: an items, a not or a property
	// under a property named allOf is no entry.
	_, err := strconv.Atoi(at[i+1:])
	return err == nil
}

// refKey returns the place in schema, which writes a reference, of the key
// naming it, and false where none does: the model holds a $ref merged in past
// nodeview.MergeDepthLimit, which schema's keys do not list (Truncated).
func refKey(schema *Schema) (int, bool) {
	i, _ := schema.find(schema.Ref.Keyword)
	return i, i >= 0
}

// own returns the schema object js holds as its statement reads it: the
// object itself, or, for one writing a reference beside other keys
// (conjoins), the rest of it, whose reference is the statement's first entry.
func (t *MemoizingSchemaReader) own(js *model) *Schema {
	schema := t.read(js)
	if conjoins(schema) {
		return t.wordsOf(schema)
	}
	return schema
}

// wordsOf returns schema, which conjoins and a position writes, less its
// reference (lessReference), and the reference as its BesideRef: the
// position's statement lists the two. Of builds it once.
func (t *MemoizingSchemaReader) wordsOf(schema *Schema) *Schema {
	schemaValue := lessReference(schema)
	schemaValue.BesideRef = t.reference(schema)
	return schemaValue
}

// restOf returns schema, an allOf entry that conjoins, less its reference
// (lessReference): the conjunct its composition lists after that reference,
// as `{allOf: [{$ref: …}, {…}]}` lists it. It writes no reference, its node
// holding its keys alone, so no reader of the composition's conjuncts reads
// the reference twice; it names it WrittenBeside, for a reader of how its
// keys are spelt (docs/compilers/openapi.md#ref-use-site).
func (t *MemoizingSchemaReader) restOf(schema *Schema) *Schema {
	if schemaValue, ok := t.rests[schema]; ok {
		return schemaValue
	}
	otherSchema := lessReference(schema)
	otherSchema.WrittenBeside = t.reference(schema)
	content := make([]*yaml.Node, 0, 2*len(otherSchema.Keys))
	for _, key := range otherSchema.Keys {
		content = append(content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key.Name}, key.Node)
	}
	otherSchema.Node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: content}
	t.rests[schema] = otherSchema
	return otherSchema
}

// lessReference returns a copy of schema, which conjoins, holding every key but
// the one writing its reference, and every slot it fills. It shares schema's
// values.
func lessReference(schema *Schema) *Schema {
	schemaValue := *schema
	schemaValue.Ref, schemaValue.of = nil, nil
	schemaValue.Keys = slices.DeleteFunc(slices.Clone(schema.Keys), func(key Key) bool { return key.Name == schema.Ref.Keyword })
	index(&schemaValue)
	return &schemaValue
}

// reference returns the reference schema, which conjoins, writes, as a schema
// object writing it alone, its node too, and its statement, which lists no
// entry. A reference no key of schema names is read from its text.
func (t *MemoizingSchemaReader) reference(schema *Schema) *Schema {
	if schemaValue, ok := t.references[schema]; ok {
		return schemaValue
	}
	otherSchema := &Schema{At: schema.At, Ref: schema.Ref, m: schema.m}
	value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: schema.Ref.Text}
	if i, named := refKey(schema); named {
		otherSchema.Keys, value = []Key{schema.Keys[i]}, schema.Keys[i].Node
	}
	// Its node writes the reference alone, so it reads as the entry
	// `{$ref: …}` would, not as the object it is written in.
	otherSchema.Node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: schema.Ref.Keyword}, value,
	}}
	otherSchema.of = &Statement{Schema: otherSchema}
	t.references[schema] = otherSchema
	return otherSchema
}

// open makes schema's struct of slot, where it has none yet: a key of the
// slot's keywords opens it, whether or not its value is read.
func open(schema *Schema, slot specrules.Slot) {
	switch {
	case slot == specrules.SlotKinds && schema.Kinds == nil:
		schema.Kinds = &Kinds{}
	case slot == specrules.SlotValues && schema.Values == nil:
		schema.Values = &Values{}
	case slot == specrules.SlotFormat && schema.Format == nil:
		schema.Format = &Format{}
	case slot == specrules.SlotNumber && schema.Number == nil:
		schema.Number = &Number{}
	case slot == specrules.SlotText && schema.Text == nil:
		schema.Text = &Text{}
	case slot == specrules.SlotArray && schema.Array == nil:
		schema.Array = &Array{}
	case slot == specrules.SlotObject && schema.Object == nil:
		schema.Object = &Object{}
	case slot == specrules.SlotChoice && schema.Choice == nil:
		schema.Choice = &Choice{}
	case slot == specrules.SlotLogic && schema.Logic == nil:
		schema.Logic = &Logic{}
	case slot == specrules.SlotNotes && schema.Notes == nil:
		schema.Notes = &Notes{}
	}
}

// isNull reports whether node, its alias followed, is a YAML null.
func isNull(node *yaml.Node) bool {
	node = nodeview.Deref(node)
	return node != nil && node.Kind == yaml.ScalarNode && node.ShortTag() == "!!null"
}
