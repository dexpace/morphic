package census

import (
	"errors"
	"reflect"
	"strconv"

	"github.com/dexpace/morphic/ir"
)

// ErrNilDocument is returned when a census is asked to count no document.
var ErrNilDocument = errors.New("census: nil document")

// tally is how many documents set a fact and how many times all of them did.
type tally struct {
	docs      int
	instances int
}

// valueKey names one value of one enumeration, by the raw spelling.
type valueKey struct {
	enum  string
	value string
}

// Census counts the facts of a Universe that the documents added to it set.
// The zero value is not usable; build one with New. It is not safe for
// concurrent use.
type Census struct {
	universe   Universe
	documents  int
	truncated  int
	noDocument int
	members    map[string]tally
	values     map[valueKey]tally
}

// New returns an empty census of the universe u.
func New(u Universe) *Census {
	return &Census{universe: u, members: map[string]tally{}, values: map[valueKey]tally{}}
}

// NoDocument counts a run that produced no document, which Add never sees.
func (c *Census) NoDocument() {
	c.noDocument++
}

// Add counts the facts doc sets. A walk that reaches the depth bound of
// [ir.MaxWalkDepth] still counts what it reached, and is counted as truncated.
func (c *Census) Add(doc *ir.Document) error {
	if doc == nil {
		return ErrNilDocument
	}
	c.add(doc)
	return nil
}

// add counts the facts the value root holds, whatever its type.
func (c *Census) add(root any) {
	d := &docCount{universe: c.universe, members: map[string]int{}, values: map[valueKey]int{}}
	truncated := ir.WalkValues(root, ir.DocumentPath, d.visit)

	c.documents++
	if truncated {
		c.truncated++
	}
	merge(c.members, d.members)
	merge(c.values, d.values)
}

// merge folds the instance counts of one document into the running tallies:
// each fact the document set gains one document and its instances.
func merge[K comparable](into map[K]tally, doc map[K]int) {
	for k, n := range doc {
		t := into[k]
		t.docs++
		t.instances += n
		into[k] = t
	}
}

// docCount is what one document's walk has counted so far.
type docCount struct {
	universe Universe
	members  map[string]int
	values   map[valueKey]int
}

// typeDefType is the sealed sum whose values carry a TypeKind no field holds.
var typeDefType = reflect.TypeFor[ir.TypeDef]()

// visit counts v. It always descends, so a walk reaches every value.
func (d *docCount) visit(v reflect.Value, _ string) bool {
	switch v.Kind() {
	case reflect.Struct:
		d.countMembers(v)
	case reflect.Interface:
		d.countKind(v)
	case reflect.String:
		d.countValue(v.Type(), v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		d.countValue(v.Type(), strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		d.countValue(v.Type(), strconv.FormatUint(v.Uint(), 10))
	default:
	}
	return true
}

// countMembers counts each member of the struct v that is set.
func (d *docCount) countMembers(v reflect.Value) {
	for _, m := range d.universe.structs[v.Type()] {
		if isSet(v.Field(m.index)) {
			d.members[m.name]++
		}
	}
}

// countKind counts the kind of a TypeDef, which the sum reports through Kind
// and no field holds. An interface that is not a TypeDef, or is nil, or that
// the walk reached behind an unexported field and so cannot read, holds none.
func (d *docCount) countKind(v reflect.Value) {
	if v.Type() != typeDefType || !v.CanInterface() {
		return
	}
	td, isTypeDef := reflect.TypeAssert[ir.TypeDef](v)
	if !isTypeDef || ir.IsNilTypeDef(td) {
		return
	}
	d.countValue(reflect.TypeFor[ir.TypeKind](), string(td.Kind()))
}

// countValue counts one value of the type t when t is an enumeration.
func (d *docCount) countValue(t reflect.Type, spelling string) {
	e, isEnum := d.universe.enums[t]
	if !isEnum {
		return
	}
	for _, held := range e.held(spelling) {
		d.values[valueKey{enum: e.name, value: held}]++
	}
}

// isSet reports whether a member's value is set: not the zero value, with a
// slice or a map set only when it holds something, so an empty one that is
// not nil is not. That leaves Operation.auth's explicit empty list, which means
// public, indistinguishable from its absence.
func isSet(v reflect.Value) bool {
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		return v.Len() > 0
	}
	return !v.IsZero()
}
