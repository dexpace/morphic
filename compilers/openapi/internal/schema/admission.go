// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"maps"
	"slices"
	"strings"

	"github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/values"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// A reading of what a schema admits is bounded twice (styleguide bounded-recursion
// rule): maxAdmissionReads caps the schemas it reads, so its depth and breadth,
// as maxNullConjuncts caps the same walk, and maxAdmissionMembers what a value set
// costs each position naming it. A reading past either says nothing, and the
// position stays the composition it would be with no reading at all, which loses
// nothing. Each position reads afresh, as a reading's answer turns on the budget
// left, so a chain of allOfs each naming the next pays up to maxAdmissionReads at
// every link.
const (
	maxAdmissionReads   = 256
	maxAdmissionMembers = 4096
)

// kindSet is a set of JSON instance kinds. An integer and a number with a
// fraction are two kinds, since `integer` admits one of them and `number` both.
type kindSet uint8

const (
	kindString kindSet = 1 << iota
	kindInteger
	kindFraction
	kindBoolean
	kindArray
	kindObject
	kindNull

	kindNumber = kindInteger | kindFraction
	kindAny    = kindString | kindNumber | kindBoolean | kindArray | kindObject | kindNull
)

// kindOfType returns the kinds a `type` member admits, and none for a spelling
// no schema defines.
func kindOfType(t oas3.SchemaType) kindSet {
	switch t {
	case oas3.SchemaTypeString:
		return kindString
	case oas3.SchemaTypeInteger:
		return kindInteger
	case oas3.SchemaTypeNumber:
		return kindNumber
	case oas3.SchemaTypeBoolean:
		return kindBoolean
	case oas3.SchemaTypeArray:
		return kindArray
	case oas3.SchemaTypeObject:
		return kindObject
	case oas3.SchemaTypeNull:
		return kindNull
	default:
		return 0
	}
}

// kindOfValue returns the JSON kind of v.
func kindOfValue(v ir.Value) kindSet {
	switch v.Kind {
	case ir.ValueString, ir.ValueSymbol, ir.ValueBytes:
		return kindString
	case ir.ValueBool:
		return kindBoolean
	case ir.ValueNull:
		return kindNull
	case ir.ValueList:
		return kindArray
	case ir.ValueObject:
		return kindObject
	case ir.ValueNumber:
		return kindOfNumber(string(v.Num))
	case ir.ValueRefKind, ir.ValueCtor:
		return 0
	default:
		return 0
	}
}

// kindOfNumber returns the kind of a decimal literal. One with an exponent may
// be integral or not, and telling them apart costs what the exponent says, so it
// answers for both.
func kindOfNumber(num string) kindSet {
	if strings.ContainsAny(num, "eE") {
		return kindNumber
	}
	if _, fraction, _ := strings.Cut(num, "."); strings.Trim(fraction, "0") != "" {
		return kindFraction
	}
	return kindInteger
}

// scalarForm is one `format` a reading holds: as written, and the primitive the
// type and format beside each other select where the table has a row for them.
type scalarForm struct {
	spelled string
	prim    ir.PrimKind
	listed  bool
}

// narrows reports whether f is the form g is, or a narrower one.
func (f scalarForm) narrows(g scalarForm) bool {
	if f.spelled == g.spelled {
		return true
	}
	return f.listed && g.listed && spelling.Relate(f.prim, g.prim) == spelling.ANarrower
}

// admitted is what a schema's keywords leave an instance able to be: its kinds,
// the values a keyword fixes where one does, and the formats and element schema
// it is held to. It compares only what these name, so two schemas differing in
// anything else, a bound or an annotation, compare equal.
type admitted struct {
	kinds kindSet
	// fixed marks a value set, values then holding each member's key (memberOf)
	// with its kind, none where the set is empty.
	fixed  bool
	values map[string]kindSet
	forms  []scalarForm
	items  *oas3.JSONSchema[oas3.Referenceable]
}

// and is what a and b admit together. The values of a set a kind beside it
// excludes stay in the set, and reachable shows them gone.
func (a admitted) and(b admitted) admitted {
	out := a
	out.kinds &= b.kinds
	out.forms = slices.Concat(a.forms, b.forms)
	if out.items == nil {
		out.items = b.items
	}
	switch {
	case a.fixed && b.fixed:
		out.values = sharedValues(a.values, b.values)
	case b.fixed:
		out.fixed, out.values = true, b.values
	}
	return out
}

// sharedValues returns the members a and b hold, walking the smaller: a value set
// is shared by every reader of it, so neither may be written to.
func sharedValues(a, b map[string]kindSet) map[string]kindSet {
	if len(b) < len(a) {
		a, b = b, a
	}
	out := make(map[string]kindSet, len(a))
	for v, kind := range a {
		if _, shared := b[v]; shared {
			out[v] = kind
		}
	}
	return out
}

// or is what either of a and b admits: a union, whose branches hold no one
// format or element schema an instance is held to.
func (a admitted) or(b admitted) admitted {
	out := admitted{kinds: a.kinds | b.kinds, fixed: a.fixed && b.fixed}
	if out.fixed {
		out.values = maps.Clone(a.values)
		maps.Copy(out.values, b.values)
	}
	return out
}

// reachable reports whether any value is left: a kind, and, under a value set, a
// member of a kind left.
func (a admitted) reachable() bool {
	if !a.fixed {
		return a.kinds != 0
	}
	for _, kind := range a.values {
		if kind&a.kinds != 0 {
			return true
		}
	}
	return false
}

// kindsWithin reports whether a's kinds are b's, which says nothing of null:
// a reference's Nullable holds that, read on its own (nulls).
func (a admitted) kindsWithin(b admitted) bool { return a.kinds&^kindNull&^b.kinds == 0 }

// valuesWithin reports whether b fixes no value a does not fix.
func (a admitted) valuesWithin(b admitted) bool {
	within, _ := a.walkWithin(b)
	return within
}

// walkWithin is valuesWithin with how many of a's members it looked up. Those
// are distinct, so a set larger than b's holds one b lacks, answered unwalked.
func (a admitted) walkWithin(b admitted) (within bool, walked int) {
	if !b.fixed {
		return true, 0
	}
	if !a.fixed || len(a.values) > len(b.values) {
		return false, 0
	}
	for v := range a.values {
		walked++
		if _, ok := b.values[v]; !ok {
			return false, walked
		}
	}
	return true, walked
}

// narrowedTo reports whether joint, what a and the conjuncts beside it admit
// together, leaves out a kind or a value a admits.
func (a admitted) narrowedTo(joint admitted) bool {
	return a.kinds != joint.kinds || !a.valuesWithin(joint)
}

// formsWithin reports whether a is held to some form narrowing each of b's.
func (a admitted) formsWithin(b admitted) bool {
	for _, want := range b.forms {
		if !slices.ContainsFunc(a.forms, func(f scalarForm) bool { return f.narrows(want) }) {
			return false
		}
	}
	return true
}

// takes reports whether a admits the value v writes, for a default read across a
// narrowing (Use.DefaultChain). One it cannot read is no value it takes.
func (a admitted) takes(v values.Value) bool {
	key, kind, ok := memberOf(v)
	if !ok {
		return false
	}
	if a.fixed {
		_, member := a.values[key]
		return member
	}
	return kind&a.kinds != 0
}

// memberOf returns the key v compares by, as an enum compares its own entries
// (valueKey), so 1 and 1.0 are one value, with its kind, and false for a value
// it cannot read.
func memberOf(v values.Value) (string, kindSet, bool) {
	val, err := value.FromNode(v)
	if err != nil {
		return "", 0, false
	}
	key, keyed := valueKey(val, 0)
	return key, kindOfValue(val), keyed
}

// admission reads what schemas admit, across each $ref along its chain and each
// conjunct and union branch, within one budget for the whole read. It asks what
// values a schema admits and mirrors no lowering's dispatch. A reading cut short
// is told by incomplete, and says nothing.
type admission struct {
	c lowering.Ctx
	// nullable reports whether the dialect c reads its schemas in defines the
	// nullable keyword (oas.SchemaRules.Defines). A schema the read reaches
	// elsewhere is read in its own document's (definesNullableAt).
	nullable bool
	reads    budget.Budget
	members  budget.Budget
	// walked hears how many members each value set comparison of the election
	// looks up, for a test to hold it to the sets it compares; a compile leaves
	// it nil.
	walked func(int)
}

// newAdmission returns a read with the whole budget, in the dialect c reads its
// schemas in. It returns a value so the read lives on its caller's stack.
func newAdmission(c lowering.Ctx) admission {
	return admission{c: c, nullable: c.SchemaRules().Defines("nullable"),
		reads: budget.New(budget.AtMost(maxAdmissionReads)), members: budget.New(budget.AtMost(maxAdmissionMembers))}
}

// incomplete reports whether the read met what it cannot answer for: a cycle
// or more than its budget, a $ref landing on no schema, a $dynamicRef, or a
// value it cannot convert.
func (w *admission) incomplete() bool { return w.reads.Exhausted() || w.members.Exhausted() }

// whole reads js, a schema of any form written anywhere, in the dialect of the
// document holding it: what its keywords say, conjoined with its allOf, its
// unions and what its $ref lands on.
func (w *admission) whole(js *oas3.JSONSchema[oas3.Referenceable]) admitted {
	if !w.reads.Spend(1) || !annotation.Written(js) {
		return admitted{kinds: kindAny}
	}
	if js.IsBool() {
		if annotation.IsFalseSchema(js) {
			return admitted{}
		}
		return admitted{kinds: kindAny}
	}
	s := js.GetSchema()
	if declaresDynamicRef(s) {
		w.reads.Exhaust()
	}
	out := w.written(s, definesNullableAt(w.c, js))
	for _, b := range s.GetAllOf() {
		out = out.and(w.whole(b))
	}
	if branches := s.GetOneOf(); len(branches) > 0 {
		out = out.and(w.union(branches))
	}
	if branches := s.GetAnyOf(); len(branches) > 0 {
		out = out.and(w.union(branches))
	}
	if !resolve.IsRefSite(js, s) {
		return out
	}
	next := w.c.Bindings().Declared(js)
	if next == nil {
		w.reads.Exhaust()
		return out
	}
	return out.and(w.whole(next))
}

// declarationSet is a set of schemas declaring together, with every schema they
// compose, as the walk gathering them reached it, and the place of their
// `items`, nil where none writes one. The place of a property is gathered from
// the schemas the first time a claim asks for it, since a claim names few of
// those a body declares.
type declarationSet struct {
	schemas    []*oas3.Schema
	composed   []*oas3.JSONSchema[oas3.Referenceable]
	properties map[string]*place
	items      *place
}

// place is one property or element a set of schemas declares: each declaration
// of it, and what they admit together and declare of what they nest, each read
// the first time it is asked for and kept, so every claim restating the place
// shares one reading.
type place struct {
	decls []*oas3.JSONSchema[oas3.Referenceable]
	held  *admitted
	inner *declarationSet
}

// newDeclarationSet returns an empty set, the one form property can be asked
// of.
func newDeclarationSet() declarationSet {
	return declarationSet{properties: map[string]*place{}}
}

// add records s and its `items`. A nil s, a boolean schema's, declares
// nothing, and the getters read it as empty.
func (d *declarationSet) add(s *oas3.Schema) {
	d.schemas = append(d.schemas, s)
	if items := itemsSchema(s); annotation.Written(items) {
		if d.items == nil {
			d.items = &place{}
		}
		d.items.decls = append(d.items.decls, items)
	}
}

// property returns the place of the property name, nil where no schema of d
// declares it, gathered once.
func (d *declarationSet) property(name string) *place {
	if at, ok := d.properties[name]; ok {
		return at
	}
	var at *place
	for _, s := range d.schemas {
		if p, ok := s.GetProperties().Get(name); ok {
			if at == nil {
				at = &place{}
			}
			at.decls = append(at.decls, p)
		}
	}
	d.properties[name] = at
	return at
}

// declarations returns what s declares: itself, each allOf conjunct and what
// a $ref lands on, read as whole reads them and within the same budget.
func (w *admission) declarations(s *oas3.Schema) declarationSet {
	out := newDeclarationSet()
	out.add(s)
	w.declare(&out, slices.Clone(s.GetAllOf()))
	return out
}

// heldAt returns what p's declarations admit together, read once.
func (w *admission) heldAt(p *place) admitted {
	if p.held == nil {
		held := w.whole(p.decls[0])
		for _, d := range p.decls[1:] {
			held = held.and(w.whole(d))
		}
		p.held = &held
	}
	return *p.held
}

// innerAt returns what p's declarations declare together, as declarations
// reads one, read once.
func (w *admission) innerAt(p *place) declarationSet {
	if p.inner == nil {
		inner := newDeclarationSet()
		w.declare(&inner, slices.Clone(p.decls))
		p.inner = &inner
	}
	return *p.inner
}

// declare adds to out what each schema on stack declares, its allOf conjuncts
// and what its $ref lands on. A union's branches are left out, since an
// instance need match only one. A declaration the walk does not reach, past the
// budget or a $ref landing nowhere, is one fewer to restate, which errs towards
// shape. The walk keeps its own stack, so it adds no recursion cycle, and the
// order of each list is no answer.
func (w *admission) declare(out *declarationSet, stack []*oas3.JSONSchema[oas3.Referenceable]) {
	for len(stack) > 0 && w.reads.Spend(1) {
		js := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out.composed = append(out.composed, js)
		cs := js.GetSchema()
		out.add(cs)
		stack = append(stack, cs.GetAllOf()...)
		if next := w.c.Bindings().Declared(js); next != nil {
			stack = append(stack, next)
		}
	}
}

// union reads a oneOf's or anyOf's branches, any of which an instance may be.
func (w *admission) union(branches []*oas3.JSONSchema[oas3.Referenceable]) admitted {
	out := w.whole(branches[0])
	for _, b := range branches[1:] {
		out = out.or(w.whole(b))
	}
	return out
}

// definesNullableAt reports whether the dialect of the document holding js
// defines nullable: where c's bindings place js, that document's, and c's own
// for a schema they do not place.
func definesNullableAt(c lowering.Ctx, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	at, ok := c.Bindings().Site(js)
	if !ok {
		return c.SchemaRules().Defines("nullable")
	}
	return definesNullableIn(c, at.Doc)
}

// written reads the keywords s itself writes that say what a value is, in a
// dialect defining nullable where nullable says: `type`, which a `nullable`
// adds null to there, `format`, `enum`, `const` and `items`. A `properties`
// with no type beside it claims an object, as lowerUntyped reads one. A schema
// in c's document is read in w.nullable's dialect.
func (w *admission) written(s *oas3.Schema, nullable bool) admitted {
	out := admitted{kinds: writtenKinds(s, nullable)}
	// `items: true` admits what no `items` does, so it holds an element to nothing.
	if items := itemsSchema(s); annotation.Written(items) && (!items.IsBool() || annotation.IsFalseSchema(items)) {
		out.items = items
	}
	if form, ok := formOf(s); ok {
		out.forms = []scalarForm{form}
		if form.listed && form.prim.StrictlyWithin(ir.PrimInteger) {
			out.kinds &= kindInteger | kindNull
		}
	}
	if enumWritten(s) {
		out = out.and(w.valueSet(valueSetOf{s, "enum"}, s.GetEnum()))
	}
	if s.GetConst() != nil {
		out = out.and(w.valueSet(valueSetOf{s, "const"}, []values.Value{s.GetConst()}))
	}
	return out
}

// writtenKinds reads a schema's `type`, or a type implied by what it declares,
// with the null a `nullable: true` adds to it, beside a $ref as anywhere, where
// the dialect defines the keyword, as nullable says. A null `type` writes none
// (writesType).
func writtenKinds(s *oas3.Schema, nullable bool) kindSet {
	types := s.GetType()
	if !writesType(s) {
		if props := s.GetProperties(); props != nil && props.Len() > 0 {
			return kindObject
		}
		return kindAny
	}
	var out kindSet
	for _, t := range types {
		out |= kindOfType(t)
	}
	if nullable && s.Nullable != nil && *s.Nullable {
		out |= kindNull
	}
	return out
}

// formOf returns the format s holds a value to. The redaction names no shape.
func formOf(s *oas3.Schema) (scalarForm, bool) {
	format := s.GetFormat()
	if format == "" || annotation.DeclaresRedaction(s) {
		return scalarForm{}, false
	}
	prim, listed := formatPrimitive(s)
	return scalarForm{spelled: format, prim: prim, listed: listed}, true
}

// valueSetOf keys the members one keyword of one schema writes.
type valueSetOf struct {
	schema  *oas3.Schema
	keyword string
}

// memberSet is a value set as a run remembers it, unread where a member has no
// reading.
type memberSet struct {
	values map[string]kindSet
	kinds  kindSet
	unread bool
}

// valueSet reads the members of an enum or a const. Reading them is remembered,
// so a shared enum costs its members once and not at each position naming it, but
// spending is not: a read's verdict turns on what it spends, so a remembered set
// must cut a read as reading it would, or lowering order would decide.
func (w *admission) valueSet(key valueSetOf, members []values.Value) admitted {
	if !w.members.Spend(len(members)) {
		return admitted{fixed: true}
	}
	set := lowering.Remembered(w.c, key, func() memberSet { return readMembers(members) })
	if set.unread {
		w.members.Exhaust()
	}
	return admitted{kinds: set.kinds, fixed: true, values: set.values}
}

// readMembers reads each member's key with its kind.
func readMembers(members []values.Value) memberSet {
	out := memberSet{values: make(map[string]kindSet, len(members))}
	for _, m := range members {
		key, kind, ok := memberOf(m)
		if !ok {
			return memberSet{unread: true}
		}
		out.values[key] = kind
		out.kinds |= kind
	}
	return out
}

// within reports whether every value a admits b admits, so far as what a reading
// holds can say.
func (w *admission) within(a, b admitted) bool {
	return a.kindsWithin(b) && a.valuesWithin(b) && a.formsWithin(b) && w.itemsWithin(a, b)
}

// itemsWithin reports whether a's elements are all b's elements can be.
func (w *admission) itemsWithin(a, b admitted) bool {
	if b.items == nil || a.items == b.items {
		return true
	}
	return a.items != nil && w.restates(w.whole(a.items), a.items, b.items)
}

// restates reports whether what held admits, read from from, says all that js
// writes: the same schema written twice, nothing a keyword of js adds, or no value
// at all, which is within anything. A keyword no reading compares, a $ref among
// them, says what it cannot tell, and a null one says nothing.
func (w *admission) restates(held admitted, from, js *oas3.JSONSchema[oas3.Referenceable]) bool {
	if !held.reachable() || w.sameText(from, js) {
		return true
	}
	s := js.GetSchema()
	if s == nil || w.reads.Exhausted() {
		return false
	}
	return !slices.ContainsFunc(rawMappingKeys(s.GetRootNode()), func(key string) bool {
		return !annotation.WritesKeywordLeftOut(s, key) && !w.holds(held, s, key)
	})
}

// sameText reports whether a and b, both written in one document, write the
// same JSON, read against one base where it writes a relative $ref: under an
// $id naming another URI, the same spelling resolves elsewhere. A copy in
// another document is read apart: its $refs resolve against that document, and
// its dialect may differ (docs/compilers/openapi.md#openapi-versions).
func (w *admission) sameText(a, b *oas3.JSONSchema[oas3.Referenceable]) bool {
	at, placed := w.c.Bindings().Site(a)
	bt, placedB := w.c.Bindings().Site(b)
	text := w.text(a)
	if !placed || !placedB || at.Doc != bt.Doc || text == nil || !bytes.Equal(text, w.text(b)) {
		return false
	}
	// The texts are remembered, so comparing them first leaves a base, which
	// walks the document, to the rare pair written alike.
	if !annotation.WritesReference(text) {
		return true
	}
	base, known := w.c.Bindings().Table().Base(at)
	other, knownB := w.c.Bindings().Table().Base(bt)
	return known && knownB && base == other
}

// textOf keys what text rendered of one schema.
type textOf struct {
	schema *oas3.JSONSchema[oas3.Referenceable]
}

// text renders js, nil where it cannot be rendered. Every position restating a
// shared schema asks, so it is rendered once a run.
func (w *admission) text(js *oas3.JSONSchema[oas3.Referenceable]) ir.RawValue {
	return lowering.Remembered(w.c, textOf{js}, func() ir.RawValue {
		raw, err := annotation.RawFromNode(js.GetCore().GetRootNode())
		if err != nil {
			return nil
		}
		return raw
	})
}

// holds reports whether what held admits says all that s's keyword key does, so
// a position lowered to held loses nothing leaving it unread.
func (w *admission) holds(held admitted, s *oas3.Schema, key string) bool {
	switch key {
	case "type":
		return held.kindsWithin(admitted{kinds: writtenKinds(s, w.nullable)})
	case "format":
		return held.formsWithin(w.written(s, w.nullable))
	case "enum", "const":
		return held.valuesWithin(w.written(s, w.nullable))
	case "items":
		return w.itemsWithin(held, w.written(s, w.nullable))
	default:
		return false
	}
}
