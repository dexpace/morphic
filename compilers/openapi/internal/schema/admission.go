// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"bytes"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/spelling"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// A position's reading of what a schema admits is cut twice: past
// maxAdmissionReads schemas taken in, each counted per path to it, and past
// maxAdmissionMembers value-set members. A reading past either says nothing, and
// the position stays the composition it would be with no reading at all, which
// loses nothing. What each statement admits is read once a run (admissionOf),
// and a position spends what that read takes in, so its verdict is the one a
// read of its own would reach.
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
func kindOfType(t statement.Kind) kindSet {
	switch t {
	case statement.KindString:
		return kindString
	case statement.KindInteger:
		return kindInteger
	case statement.KindNumber:
		return kindNumber
	case statement.KindBoolean:
		return kindBoolean
	case statement.KindArray:
		return kindArray
	case statement.KindObject:
		return kindObject
	case statement.KindNull:
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
	case ir.ValueRefKind, ir.ValueCtor, ir.ValueInf, ir.ValueNegInf, ir.ValueNaN:
		// A reference, a constructor and a non-finite number are no JSON
		// instance's value.
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
	// with its kind, none where the set is empty, and joined the sets a union
	// adds to it, each still the branch's own: a union shares its branches'
	// sets rather than copying them, and none is written to.
	fixed  bool
	values map[string]kindSet
	joined []map[string]kindSet
	forms  []scalarForm
	items  *statement.Statement
}

// hasMember reports whether a's value set holds the member keyed key.
func (a admitted) hasMember(key string) bool {
	if _, ok := a.values[key]; ok {
		return true
	}
	for _, set := range a.joined {
		if _, ok := set[key]; ok {
			return true
		}
	}
	return false
}

// members yields each member of a's value set with its kind, one two joined
// sets hold once for each.
func (a admitted) members(yield func(string, kindSet) bool) {
	for v, kind := range a.values {
		if !yield(v, kind) {
			return
		}
	}
	for _, set := range a.joined {
		for v, kind := range set {
			if !yield(v, kind) {
				return
			}
		}
	}
}

// size is how many members a's sets hold together, one two of them hold
// counted for each.
func (a admitted) size() int {
	n := len(a.values)
	for _, set := range a.joined {
		n += len(set)
	}
	return n
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
		out.values, out.joined = sharedValues(a, b), nil
	case b.fixed:
		out.fixed, out.values, out.joined = true, b.values, b.joined
	}
	return out
}

// sharedValues returns the members a's and b's value sets hold, walking the
// smaller: a value set is shared by every reader of it, so neither may be
// written to.
func sharedValues(a, b admitted) map[string]kindSet {
	if b.size() < a.size() {
		a, b = b, a
	}
	out := make(map[string]kindSet, a.size())
	for v, kind := range a.members {
		if b.hasMember(v) {
			out[v] = kind
		}
	}
	return out
}

// or is what either of a and b admits: a union, whose branches hold no one
// format or element schema an instance is held to, and whose value set joins
// theirs.
func (a admitted) or(b admitted) admitted {
	out := admitted{kinds: a.kinds | b.kinds, fixed: a.fixed && b.fixed}
	if out.fixed {
		out.values, out.joined = a.values, slices.Concat(a.joined, []map[string]kindSet{b.values}, b.joined)
	}
	return out
}

// reachable reports whether any value is left: a kind, and, under a value set, a
// member of a kind left.
func (a admitted) reachable() bool {
	if !a.fixed {
		return a.kinds != 0
	}
	for _, kind := range a.members {
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

// walkWithin is valuesWithin with how many of a's members it looked up. The
// members of one set are distinct, so where neither joins another set, one
// larger than b's holds one b lacks, answered unwalked.
func (a admitted) walkWithin(b admitted) (within bool, walked int) {
	if !b.fixed {
		return true, 0
	}
	if !a.fixed || len(a.joined) == 0 && len(b.joined) == 0 && len(a.values) > len(b.values) {
		return false, 0
	}
	for v := range a.members {
		walked++
		if !b.hasMember(v) {
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
// narrowing (Use.Default). One it cannot read is no value it takes.
func (a admitted) takes(v *yaml.Node) bool {
	key, kind, ok := memberOf(v)
	if !ok {
		return false
	}
	if a.fixed {
		return a.hasMember(key)
	}
	return kind&a.kinds != 0
}

// memberOf returns the key v compares by, as an enum compares its own entries
// (valueKey), so 1 and 1.0 are one value, with its kind, and false for a value
// it cannot read.
func memberOf(v *yaml.Node) (string, kindSet, bool) {
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

// of is what js, a schema of any form written anywhere, admits, as the run
// reads it once (admissionOf): what its keywords say, in the dialect of the
// document holding it, conjoined with its allOf, its unions and what its $ref
// lands on. It spends what that read takes in, so a read past either cap, or
// one admissionOf could not settle, is cut short here as reading it would be.
func (w *admission) of(js *statement.Statement) admitted {
	r := admissionOf(w.c, js)
	w.reads.Spend(r.reads)
	w.members.Spend(r.members)
	if r.cut || r.dynamic {
		w.reads.Exhaust()
	}
	return r.admitted
}

// lowered is what js admits as the node it lowers to does: what w reads of it,
// narrowed to the kinds the kind-inference policy read off shape keywords
// where nothing js declares names a kind (loweredAs). An election weighs what
// each conjunct declares, and reads admission.of; a reader asking whether a
// position already says what a keyword does reads this.
func lowered(w *admission, js *statement.Statement) admitted {
	return loweredAs(w.c, js, w.of(js))
}

// loweredAs is read, what js admits, narrowed to the kinds js lowers to where
// the kind-inference policy read them (readOne), as the node it lowers to
// excludes every other kind. A read cut short narrows nothing.
func loweredAs(c lowering.Ctx, js *statement.Statement, read admitted) admitted {
	if r := admissionOf(c, js); r.inferred && !r.cut {
		read.kinds &= r.kinds
	}
	return read
}

// writtenLowered is what s's own keywords admit as w reads them
// (admission.written), narrowed, where it writes no type, to the kinds it
// lowers to (untypedKinds): those its conjuncts declare where its keywords
// speak of them, else those the kind-inference policy reads off them. So a
// body beside branches each declaring an object admits what it would writing
// `type: object`.
func writtenLowered(w *admission, s *statement.Schema, nullable bool) admitted {
	read := w.written(s, nullable)
	if writesType(s) {
		return read
	}
	if kinds, _ := untypedKinds(w.c, s); kinds != 0 {
		read.kinds &= kinds
	}
	return read
}

// declarationSet is a set of schemas declaring together, with every schema they
// compose, as the walk gathering them reached it, and the place of their
// `items`, nil where none writes one. The place of a property is gathered from
// the schemas the first time a claim asks for it, since a claim names few of
// those a body declares.
type declarationSet struct {
	schemas    []*statement.Schema
	composed   []*statement.Statement
	properties map[string]*place
	items      *place
}

// place is one property or element a set of schemas declares: each declaration
// of it, and what they admit together and declare of what they nest, each read
// the first time it is asked for and kept, so every claim restating the place
// shares one reading.
type place struct {
	decls []*statement.Statement
	held  *admitted
	inner *declarationSet
}

// newDeclarationSet returns an empty set, the one form property can be asked
// of.
func newDeclarationSet() declarationSet {
	return declarationSet{properties: map[string]*place{}}
}

// add records s and its `items`. An s holding no schema object, a boolean
// schema's, declares nothing: it writes no `items` and no property
// (statement.Schema.Property).
func (d *declarationSet) add(t *statement.Table, s *statement.Schema) {
	d.schemas = append(d.schemas, s)
	if items := t.Sub(itemsSchema(s)); items.Truth != statement.Unwritten {
		if d.items == nil {
			d.items = &place{}
		}
		d.items.decls = append(d.items.decls, items)
	}
}

// property returns the place of the property name, nil where no schema of d
// declares it, gathered once and read from t.
func (d *declarationSet) property(t *statement.Table, name string) *place {
	if at, ok := d.properties[name]; ok {
		return at
	}
	var at *place
	for _, s := range d.schemas {
		if p, ok := s.Property(name); ok {
			if at == nil {
				at = &place{}
			}
			at.decls = append(at.decls, t.Sub(p))
		}
	}
	d.properties[name] = at
	return at
}

// declarations returns what s declares: itself, each allOf conjunct and what
// a $ref lands on, read as of reads them and within the same budget.
func (w *admission) declarations(s *statement.Schema) declarationSet {
	out := newDeclarationSet()
	out.add(w.c.Statements(), s)
	w.declare(&out, statementsOf(w.c, allOfIn(s)))
	return out
}

// heldAt returns what p's declarations admit together, read once.
func (w *admission) heldAt(p *place) admitted {
	if p.held == nil {
		held := lowered(w, p.decls[0])
		for _, d := range p.decls[1:] {
			held = held.and(lowered(w, d))
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
func (w *admission) declare(out *declarationSet, stack []*statement.Statement) {
	for len(stack) > 0 && w.reads.Spend(1) {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out.composed = append(out.composed, st)
		out.add(w.c.Statements(), st.Schema)
		for _, b := range st.AllOf {
			stack = append(stack, w.c.Statements().Sub(b))
		}
		if next, _ := declaredOf(w.c, st); next != nil {
			stack = append(stack, next)
		}
	}
}

// statementsOf returns the statements of the schemas list names, in its
// order.
func statementsOf(c lowering.Ctx, list []statement.Sub) []*statement.Statement {
	out := make([]*statement.Statement, len(list))
	for i, b := range list {
		out[i] = c.Statements().Sub(b)
	}
	return out
}

// union reads a oneOf's or anyOf's branches, any of which an instance may be.
func (w *admission) union(branches []statement.Sub) admitted {
	out := w.of(w.c.Statements().Sub(branches[0]))
	for _, b := range branches[1:] {
		out = out.or(w.of(w.c.Statements().Sub(b)))
	}
	return out
}

// definesNullableAt reports whether the dialect of the document holding js
// defines nullable: where c's bindings place js, that document's, and c's own
// for a schema they do not place.
func definesNullableAt(c lowering.Ctx, js *statement.Statement) bool {
	at, ok := siteOf(js.Schema)
	if !ok {
		return c.SchemaRules().Defines("nullable")
	}
	return definesNullableIn(c, at.Doc)
}

// written reads the keywords s itself writes that say what a value is, in a
// dialect defining nullable where nullable says: `type`, which a `nullable`
// adds null to there, `format`, `enum`, `const` and `items`. A schema in c's
// document is read in w.nullable's dialect.
func (w *admission) written(s *statement.Schema, nullable bool) admitted {
	out := admitted{kinds: writtenKinds(s, nullable)}
	// `items: true` admits what no `items` does, so it holds an element to nothing.
	if items := w.c.Statements().Sub(itemsSchema(s)); items.Truth == statement.Stated || items.Truth.ReadsFalse() {
		out.items = items
	}
	if form, ok := formOf(s); ok {
		out.forms = []scalarForm{form}
		if form.listed && form.prim.StrictlyWithin(ir.PrimInteger) {
			out.kinds &= kindInteger | kindNull
		}
	}
	if enumWritten(s) {
		out = out.and(w.valueSet(valueSetOf{s, "enum"}, enumIn(s)))
	}
	if constant := constIn(s); constant != nil {
		out = out.and(w.valueSet(valueSetOf{s, "const"}, []*yaml.Node{constant}))
	}
	return out
}

// writtenKinds reads a schema's `type`, with the null a `nullable: true` adds
// to it, beside a $ref as anywhere, where the dialect defines the keyword, as
// nullable says. A schema writing none, a null `type` among them (writesType),
// admits every kind, whatever shape keywords it writes: each passes an
// instance of another kind (docs/compilers/openapi.md#shape-applicator).
func writtenKinds(s *statement.Schema, nullable bool) kindSet {
	if !writesType(s) {
		return kindAny
	}
	var out kindSet
	for _, t := range kindsIn(s) {
		out |= kindOfType(t)
	}
	if nullable && nullableTrue(s) {
		out |= kindNull
	}
	return out
}

// formOf returns the format s holds a value to. The redaction names no shape.
func formOf(s *statement.Schema) (scalarForm, bool) {
	format := namedFormat(s)
	if format == "" || annotation.DeclaresRedaction(s) {
		return scalarForm{}, false
	}
	prim, listed := formatPrimitive(s)
	return scalarForm{spelled: format, prim: prim, listed: listed}, true
}

// valueSetOf keys the members one keyword of one schema writes.
type valueSetOf struct {
	schema  *statement.Schema
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
func (w *admission) valueSet(key valueSetOf, members []*yaml.Node) admitted {
	if !w.members.Spend(len(members)) {
		return admitted{fixed: true}
	}
	set := memberSetOf(w.c, key, members)
	if set.unread {
		w.members.Exhaust()
	}
	return admitted{kinds: set.kinds, fixed: true, values: set.values}
}

// memberSetOf returns the members of the set key names, members, read once a
// run.
func memberSetOf(c lowering.Ctx, key valueSetOf, members []*yaml.Node) memberSet {
	return lowering.Remembered(c, key, func() memberSet { return readMembers(members) })
}

// valueKinds returns the kinds the enum and the const s writes leave, null
// aside: every kind where it writes neither, and none for a set no reading
// converts, which cuts the read (readOwn).
func valueKinds(c lowering.Ctx, s *statement.Schema) kindSet {
	kinds := anyInstance
	if enumWritten(s) {
		kinds &= memberSetOf(c, valueSetOf{s, "enum"}, enumIn(s)).kinds
	}
	if constant := constIn(s); constant != nil {
		kinds &= memberSetOf(c, valueSetOf{s, "const"}, []*yaml.Node{constant}).kinds
	}
	return kinds
}

// readMembers reads each member's key with its kind.
func readMembers(members []*yaml.Node) memberSet {
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
	return a.items != nil && w.restates(w.of(a.items), a.items, b.items)
}

// restates reports whether what held admits, read from from, says all that js
// writes: the same schema written twice, nothing a keyword of js adds, or no value
// at all, which is within anything. A keyword no reading compares, a $ref among
// them, says what it cannot tell, and a null one says nothing.
func (w *admission) restates(held admitted, from, js *statement.Statement) bool {
	if !held.reachable() || w.sameText(from, js) {
		return true
	}
	if js.Truth != statement.Stated || w.reads.Exhausted() {
		return false
	}
	return !slices.ContainsFunc(js.Keys, func(k statement.Key) bool {
		return !annotation.WritesKeywordLeftOut(js.Schema, k.Name) && !w.holds(held, js.Schema, k.Name)
	})
}

// sameText reports whether a and b, both written in one document, write the
// same JSON, read against one base where it writes a relative $ref: under an
// $id naming another URI, the same spelling resolves elsewhere. A copy in
// another document is read apart: its $refs resolve against that document, and
// its dialect may differ (docs/compilers/openapi.md#openapi-versions). A schema
// placed nowhere, a nil one among them, is written alike with none.
func (w *admission) sameText(a, b *statement.Statement) bool {
	at, placed := siteOf(a.Schema)
	bt, placedB := siteOf(b.Schema)
	if !placed || !placedB || at.Doc != bt.Doc {
		return false
	}
	text := w.text(a)
	if text == nil || !bytes.Equal(text, w.text(b)) {
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
	schema *statement.Statement
}

// text renders js, nil where it cannot be rendered. Every position restating a
// shared schema asks, so it is rendered once a run.
func (w *admission) text(js *statement.Statement) ir.RawValue {
	return lowering.Remembered(w.c, textOf{js}, func() ir.RawValue {
		raw, err := annotation.RawFromNode(js.Node)
		if err != nil {
			return nil
		}
		return raw
	})
}

// holds reports whether what held admits says all that s's keyword key does, so
// a position lowered to held loses nothing leaving it unread.
func (w *admission) holds(held admitted, s *statement.Schema, key string) bool {
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
