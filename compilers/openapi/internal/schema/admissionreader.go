// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package schema

import (
	"slices"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/statement"
	"github.com/dexpace/morphic/ir"
)

// A position's reading of what a schema admits is cut twice: past
// maxAdmissionReads schemas taken in, each counted per path to it, and past
// maxAdmissionMembers value-set members. Past either, what an election compares
// says nothing (admission.past), and the position stays the composition it would
// be with no reading at all, which loses nothing; the kinds the read admits and
// lowers to stay whole. What each statement admits is read once a run
// (admissionOf), and a position spends what that read takes in, so its verdict
// is the one a read of its own would reach.
const (
	maxAdmissionReads   = 256
	maxAdmissionMembers = 4096
)

// admissionReader reads what schemas admit, across each $ref along its chain and each
// conjunct and union branch, within one budget for the whole read. It asks what
// values a schema admits and mirrors no lowering's dispatch. A reading cut short
// is told by incomplete, and says nothing.
type admissionReader struct {
	c lowering.Ctx
	// nullable reports whether the dialect c reads its schemas in defines the
	// nullable keyword (specrules.SchemaRules.Defines). A schema the read
	// reaches elsewhere is read in its own document's (definesNullableAt).
	nullable bool
	// cut marks a read that met what no cap answers for: a cycle, a $ref
	// landing on no schema or a value it cannot convert. incomplete holds it
	// too, and a read past a cap besides.
	cut     bool
	reads   budget.Budget
	members budget.Budget
	// walked hears how many members each value set comparison of the election
	// looks up, for a test to hold it to the sets it compares; a compile leaves
	// it nil.
	walked func(int)
	// compared hears each comparison of two readings (within), for a test to
	// hold an election to one per pair of conjuncts; a compile leaves it nil.
	compared func()
}

// newAdmissionReader returns a read with the whole budget, in the dialect
// loweringContext reads its schemas in. It returns a value so the read lives on
// its caller's stack.
func newAdmissionReader(loweringContext lowering.Ctx) admissionReader {
	return admissionReader{c: loweringContext, nullable: loweringContext.SchemaRules().Defines("nullable"),
		reads: budget.New(budget.AtMost(maxAdmissionReads)), members: budget.New(budget.AtMost(maxAdmissionMembers))}
}

// incomplete reports whether the read met what it cannot answer for: a cycle
// or more than its budget, a $ref landing on no schema, or a value it cannot
// convert.
func (w *admissionReader) incomplete() bool { return w.reads.Exhausted() || w.members.Exhausted() }

// of is what js, a schema of any form written anywhere, admits, as the run
// reads it once (admissionOf): what its keywords say, in the dialect of the
// document holding it, conjoined with its allOf, its unions and what its $ref
// lands on. It spends what that read takes in, so a read past either cap, or
// one admissionOf could not settle, is cut short here as reading it would be.
// A $dynamicRef the read meets masks no kind: one the position expands is read
// as what it names, and any other admits every kind.
func (w *admissionReader) of(js *statement.Statement) admitted {
	admission := admissionOf(w.c, js)
	w.reads.Spend(admission.reads)
	w.members.Spend(admission.members)
	if admission.cut {
		w.cut = true
		w.reads.Exhaust()
	}
	return admission.admitted
}

// lowered is what js admits as the node it lowers to does: what
// admissionReaderValue reads of it, narrowed to the kinds the kind-inference
// policy read off shape keywords where nothing js declares names a kind
// (loweredAs). An election weighs what each conjunct declares, and reads
// admissionReader.of; a reader asking whether a position already says what a
// keyword does reads this.
func lowered(admissionReaderValue *admissionReader, js *statement.Statement) admitted {
	return loweredAs(admissionReaderValue.c, js, admissionReaderValue.of(js))
}

// loweredAs is read, what js admits, narrowed to the kinds js lowers to where
// the kind-inference policy read them (lowersTo), as the node it lowers to
// excludes every other kind.
func loweredAs(loweringContext lowering.Ctx, js *statement.Statement, read admitted) admitted {
	if kinds, inferred := lowersTo(loweringContext, js); inferred {
		read.kinds &= kinds
	}
	return read
}

// lowersTo returns the kinds js lowers to, null aside, and whether the
// kind-inference policy read them: what the run's admission of it settles
// (settleKinds), and where that read is cut short, what the policy reads off
// the keywords js writes itself (claimedKinds), else every kind. The election,
// the readers of what a position already says and the lowering of a schema
// naming no kind each read a statement's kinds here.
func lowersTo(loweringContext lowering.Ctx, js *statement.Statement) (kindSet, bool) {
	admission := admissionOf(loweringContext, js)
	if !admission.cut {
		return admission.kinds, admission.inferred
	}
	if claims := claimedKinds(loweringContext, js.Schema); claims != 0 {
		return claims, true
	}
	return anyInstance, false
}

// writtenLowered is what schema's own keywords admit as admissionReaderValue
// reads them (admissionReader.written), narrowed, where it writes no type, to
// the kinds it lowers to (untypedKinds): those its conjuncts declare where its
// keywords speak of them, else those the kind-inference policy reads off them.
// So a body beside branches each declaring an object admits what it would
// writing `type: object`.
func writtenLowered(admissionReaderValue *admissionReader, schema *statement.Schema, nullable bool) admitted {
	read := admissionReaderValue.written(schema, nullable)
	if writesType(schema) {
		return read
	}
	if kinds, _ := untypedKinds(admissionReaderValue.c, schema); kinds != 0 {
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

// add records schema and its `items`. An schema holding no schema object, a
// boolean schema's, declares nothing: it writes no `items` and no property
// (statement.Schema.Property).
func (d *declarationSet) add(memoizingSchemaReader *statement.MemoizingSchemaReader, schema *statement.Schema) {
	d.schemas = append(d.schemas, schema)
	if items := memoizingSchemaReader.Subschema(itemsSchema(schema)); items.Truth != statement.Unwritten {
		if d.items == nil {
			d.items = &place{}
		}
		d.items.decls = append(d.items.decls, items)
	}
}

// property returns the place of the property name, nil where no schema of d
// declares it, gathered once and read from memoizingSchemaReader.
func (d *declarationSet) property(memoizingSchemaReader *statement.MemoizingSchemaReader, name string) *place {
	if at, ok := d.properties[name]; ok {
		return at
	}
	var at *place
	for _, schema := range d.schemas {
		if subschema, ok := schema.Property(name); ok {
			if at == nil {
				at = &place{}
			}
			at.decls = append(at.decls, memoizingSchemaReader.Subschema(subschema))
		}
	}
	d.properties[name] = at
	return at
}

// declarationsOf returns what schema declares: itself, each allOf conjunct and
// what a $ref lands on, read as admissionReaderValue reads them and within the
// same budget.
func declarationsOf(admissionReaderValue *admissionReader, schema *statement.Schema) declarationSet {
	out := newDeclarationSet()
	out.add(admissionReaderValue.c.Statements(), schema)
	out.declare(admissionReaderValue, conjunctsIn(admissionReaderValue.c, schema))
	return out
}

// admits returns what p's declarations admit together, as admissionReaderValue
// reads them, read once.
func (p *place) admits(admissionReaderValue *admissionReader) admitted {
	if p.held == nil {
		held := lowered(admissionReaderValue, p.decls[0])
		for _, schemaStatement := range p.decls[1:] {
			held = held.and(lowered(admissionReaderValue, schemaStatement))
		}
		p.held = &held
	}
	return *p.held
}

// declares returns what p's declarations declare together, as declarationsOf
// reads one, read once.
func (p *place) declares(admissionReaderValue *admissionReader) declarationSet {
	if p.inner == nil {
		inner := newDeclarationSet()
		inner.declare(admissionReaderValue, slices.Clone(p.decls))
		p.inner = &inner
	}
	return *p.inner
}

// declare adds to d what each schema on stack declares, its conjuncts
// (conjunctsIn) and what its $ref lands on. A union's branches are left out, since an
// instance need match only one. A declaration the walk does not reach, past the
// budget or a $ref landing nowhere, is one fewer to restate, which errs towards
// shape. The walk keeps its own stack, so it adds no recursion cycle, and the
// order of each list is no answer.
func (d *declarationSet) declare(admissionReaderValue *admissionReader, stack []*statement.Statement) {
	for len(stack) > 0 && admissionReaderValue.reads.Spend(1) {
		st := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		d.composed = append(d.composed, st)
		d.add(admissionReaderValue.c.Statements(), st.Schema)
		stack = append(stack, conjunctsIn(admissionReaderValue.c, st.Schema)...)
		if next, _ := declaredOf(admissionReaderValue.c, st); next != nil {
			stack = append(stack, next)
		}
	}
}

// statementsOf returns the statements of the schemas list names, in its
// order.
func statementsOf(loweringContext lowering.Ctx, list []statement.Subschema) []*statement.Statement {
	out := make([]*statement.Statement, len(list))
	for i, b := range list {
		out[i] = loweringContext.Statements().Subschema(b)
	}
	return out
}

// union reads a oneOf's or anyOf's branches, any of which an instance may be.
func (w *admissionReader) union(branches []statement.Subschema) admitted {
	out := w.of(w.c.Statements().Subschema(branches[0]))
	for _, b := range branches[1:] {
		out = out.or(w.of(w.c.Statements().Subschema(b)))
	}
	return out
}

// definesNullableAt reports whether the dialect of the document holding js
// defines nullable: where loweringContext's bindings place js, that document's,
// and loweringContext's own for a schema they do not place.
func definesNullableAt(loweringContext lowering.Ctx, js *statement.Statement) bool {
	at, ok := siteOf(js.Schema)
	if !ok {
		return loweringContext.SchemaRules().Defines("nullable")
	}
	return definesNullableIn(loweringContext, at.Doc)
}

// written reads the keywords schema itself writes that say what a value is, in
// a dialect defining nullable where nullable says: `type`, which a `nullable`
// adds null to there, `format`, `enum`, `const` and `items`. A schema in c's
// document is read in w.nullable's dialect.
func (w *admissionReader) written(schema *statement.Schema, nullable bool) admitted {
	out := admitted{kinds: writtenKinds(schema, nullable)}
	// `items: true` admits what no `items` does, so it holds an element to nothing.
	if items := w.c.Statements().Subschema(itemsSchema(schema)); items.Truth == statement.Stated || items.Truth.ReadsFalse() {
		out.items = items
	}
	if form, ok := formOf(schema); ok {
		out.forms = []scalarForm{form}
		if form.listed && form.prim.StrictlyWithin(ir.PrimitiveInteger) {
			out.kinds &= kindInteger | kindNull
		}
	}
	if enumWritten(schema) {
		out = out.and(w.valueSet(valueSetOf{schema, "enum"}, schema.Enum()))
	}
	if constant := schema.Const(); constant != nil {
		out = out.and(w.valueSet(valueSetOf{schema, "const"}, []*yaml.Node{constant}))
	}
	return out
}

// writtenKinds reads a schema's `type`, with the null a `nullable: true` adds
// to it, beside a $ref as anywhere, where the dialect defines the keyword, as
// nullable says. A schema writing none, a null `type` among them (writesType),
// admits every kind, whatever shape keywords it writes: each passes an
// instance of another kind (docs/compilers/openapi.md#shape-applicator).
func writtenKinds(schema *statement.Schema, nullable bool) kindSet {
	if !writesType(schema) {
		return kindAny
	}
	var out kindSet
	for _, jsonSchemaTypeName := range schema.Types() {
		out |= kindOfType(jsonSchemaTypeName)
	}
	if nullable && nullableTrue(schema) {
		out |= kindNull
	}
	return out
}

// formOf returns the format schema holds a value to. The redaction names no
// shape.
func formOf(schema *statement.Schema) (scalarForm, bool) {
	format := schema.FormatName()
	if format == "" || annotation.DeclaresRedaction(schema) {
		return scalarForm{}, false
	}
	prim, listed := formatPrimitive(schema)
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
func (w *admissionReader) valueSet(key valueSetOf, members []*yaml.Node) admitted {
	if !w.members.Spend(len(members)) {
		return admitted{fixed: true}
	}
	set := memberSetOf(w.c, key, members)
	if set.unread {
		w.cut = true
		w.members.Exhaust()
	}
	return admitted{kinds: set.kinds, fixed: true, values: set.values}
}

// memberSetOf returns the members of the set key names, members, read once a
// run.
func memberSetOf(loweringContext lowering.Ctx, key valueSetOf, members []*yaml.Node) memberSet {
	return lowering.ReadOncePerRun(loweringContext, key, func() memberSet { return readMembers(members) })
}

// valueKinds returns the kinds the enum and the const schema writes leave, null
// aside: every kind where it writes neither, and none for a set no reading
// converts, which cuts the read (readOwn).
func valueKinds(loweringContext lowering.Ctx, schema *statement.Schema) kindSet {
	kinds := anyInstance
	if enumWritten(schema) {
		kinds &= memberSetOf(loweringContext, valueSetOf{schema, "enum"}, schema.Enum()).kinds
	}
	if constant := schema.Const(); constant != nil {
		kinds &= memberSetOf(loweringContext, valueSetOf{schema, "const"}, []*yaml.Node{constant}).kinds
	}
	return kinds
}

// readMembers reads each member's key with its kind.
func readMembers(members []*yaml.Node) memberSet {
	out := memberSet{values: make(map[string]kindSet, len(members))}
	for _, node := range members {
		key, kind, ok := memberOf(node)
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
func (w *admissionReader) within(a, b admitted) bool {
	if w.compared != nil {
		w.compared()
	}
	return a.kindsWithin(b) && a.valuesWithin(b) && a.formsWithin(b) && w.itemsWithin(a, b)
}

// itemsWithin reports whether a's elements are all b's elements can be.
func (w *admissionReader) itemsWithin(a, b admitted) bool {
	if b.items == nil || a.items == b.items {
		return true
	}
	return a.items != nil && w.restates(w.of(a.items), a.items, b.items)
}

// restates reports whether what held admits, read from from, says all that js
// writes: the same schema written twice, nothing a keyword of js adds, or no value
// at all, which is within anything. A keyword no reading compares, a $ref among
// them, says what it cannot tell, and a null one says nothing.
func (w *admissionReader) restates(held admitted, from, js *statement.Statement) bool {
	if !held.reachable() || sameText(w.c, from, js) {
		return true
	}
	if js.Truth != statement.Stated || w.reads.Exhausted() {
		return false
	}
	return !slices.ContainsFunc(js.Keys, func(key statement.Key) bool {
		return !annotation.WritesKeywordLeftOut(js.Schema, key.Name) && !w.holds(held, js.Schema, key.Name)
	})
}

// holds reports whether what held admits says all that schema's keyword key
// does, so a position lowered to held loses nothing leaving it unread.
func (w *admissionReader) holds(held admitted, schema *statement.Schema, key string) bool {
	switch key {
	case "type":
		return held.kindsWithin(admitted{kinds: writtenKinds(schema, w.nullable)})
	case "format":
		return held.formsWithin(w.written(schema, w.nullable))
	case "enum", "const":
		return held.valuesWithin(w.written(schema, w.nullable))
	case "items":
		return w.itemsWithin(held, w.written(schema, w.nullable))
	default:
		return false
	}
}
