// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package census

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/internal/jsonshape"
	"github.com/dexpace/morphic/ir"
)

// Universe is every fact a census counts, read from one package's
// declarations through the JSON shape its root reaches, never listed by hand:
// each member of its structs, the emptiness of each member whose empty value
// differs from its absence, and each value of its enumerations. The zero value
// holds none.
type Universe struct {
	pkgPath string
	// facts are the member facts in the order a report lists them.
	facts   []memberFact
	structs map[string]*structFacts
	sums    map[string]map[variantKey]valueKey
	enums   map[string]*enumeration
	// all lists the enumerations by name.
	all []*enumeration
}

// memberFact names one member, "<Struct>.<json>", and says whether its
// emptiness is a fact of its own.
type memberFact struct {
	name      string
	emptiable bool
}

// structFacts is what a census reads of one struct type: the Go name of each
// of its fields, by index, and the members among them.
type structFacts struct {
	fields  []string
	members []member
}

// member is one struct member the census counts.
type member struct {
	// index is the member's position among its struct's fields.
	index int
	memberFact
}

// variantKey names a type a sealed sum holds, and whether through a pointer.
type variantKey struct {
	name      string
	byPointer bool
}

// enumeration is a named type that declares constants: its values, each by
// its spelling as a Go literal, so a string quoted and an integer in decimal.
type enumeration struct {
	name     string
	declared map[string]constant.Value
}

// NewUniverse derives the universe of ir.Document. It reads package ir's
// declarations through go/packages, which resolves the import path from the
// working directory, so it fails outside the module, and it refuses
// declarations of another IRVersion than this build's, which would describe a
// different IR.
func NewUniverse() (Universe, error) {
	return newUniverse(ir.IRVersion)
}

// newUniverse derives the universe of ir.Document from declarations that must
// be of the IRVersion version.
func newUniverse(version string) (Universe, error) {
	root := reflect.TypeFor[ir.Document]()
	p, err := decls.Load(root.PkgPath())
	if err != nil {
		return Universe{}, fmt.Errorf("census: %w", err)
	}
	if err := checkVersion(p, version); err != nil {
		return Universe{}, err
	}
	return universeOf(p, root.Name())
}

// checkVersion refuses the package p unless its IRVersion constant is want.
func checkVersion(p *decls.Package, want string) error {
	c, isConst := p.Types.Scope().Lookup("IRVersion").(*types.Const)
	if !isConst || c.Val().Kind() != constant.String {
		return fmt.Errorf("census: %s declares no IRVersion string constant", p.Types.Path())
	}
	if got := constant.StringVal(c.Val()); got != want {
		return fmt.Errorf("census: this build reads IRVersion %s, but the declarations it read are of %s: "+
			"run it from the checkout it was built in", want, got)
	}
	return nil
}

// universeOf derives the universe of the named type root, which p declares.
// A type of another package that the root reaches is no fact of this one.
func universeOf(p *decls.Package, root string) (Universe, error) {
	shape, err := jsonshape.Read(p, root)
	if err != nil {
		return Universe{}, fmt.Errorf("census: %w", err)
	}
	u := Universe{
		pkgPath: p.Types.Path(),
		structs: map[string]*structFacts{},
		sums:    map[string]map[variantKey]valueKey{},
		enums:   map[string]*enumeration{},
	}
	var sums []jsonshape.Type
	for _, t := range shape.Types {
		if t.Named.Obj().Pkg() != p.Types {
			continue
		}
		if err := u.add(t); err != nil {
			return Universe{}, err
		}
		if t.Tag != nil {
			sums = append(sums, t)
		}
	}
	// A tag names its enumeration, which may sort after the sum.
	for _, t := range sums {
		if err := u.addSum(t); err != nil {
			return Universe{}, err
		}
	}
	slices.SortFunc(u.facts, func(a, b memberFact) int { return strings.Compare(a.name, b.name) })
	return u, nil
}

// add records the facts the type t declares: a struct's members and an
// enumeration's values.
func (u *Universe) add(t jsonshape.Type) error {
	if s, isStruct := t.Named.Underlying().(*types.Struct); isStruct {
		return u.addStruct(t, s)
	}
	if len(t.Constants) > 0 {
		return u.addEnum(t)
	}
	return nil
}

// addStruct records the members of the struct t, each under its JSON name,
// leaving out an embedding, whose members are its own. Every field must be
// one the encoder reads, since the walk a census counts by reads every field.
func (u *Universe) addStruct(t jsonshape.Type, s *types.Struct) error {
	name := t.Named.Obj().Name()
	sf := &structFacts{fields: make([]string, s.NumFields())}
	read := make([]bool, s.NumFields())
	for _, m := range t.Members {
		read[m.Index] = true
		if m.Embedded {
			continue
		}
		f := memberFact{name: name + "." + m.Name, emptiable: emptiable(m)}
		sf.members = append(sf.members, member{index: m.Index, memberFact: f})
		u.facts = append(u.facts, f)
	}
	for i := range s.NumFields() {
		sf.fields[i] = s.Field(i).Name()
		if !read[i] {
			return fmt.Errorf("census: the encoder does not write %s.%s, so a census would count what it holds",
				name, sf.fields[i])
		}
	}
	u.structs[name] = sf
	return nil
}

// emptiable reports whether an empty value of the member m means something its
// absence does not: m is a slice or a map tagged omitzero, which the encoder
// writes as [] or {} when it is empty and leaves out only when it is nil.
func emptiable(m jsonshape.Member) bool {
	switch m.Field.Type().Underlying().(type) {
	case *types.Slice, *types.Map:
		return slices.Contains(m.Options, "omitzero")
	default:
		return false
	}
}

// addEnum records the values the constants of t give it. It refuses a
// constant that is neither a string nor an integer, since no value of it could
// be counted.
func (u *Universe) addEnum(t jsonshape.Type) error {
	e := &enumeration{name: t.Named.Obj().Name(), declared: map[string]constant.Value{}}
	for _, c := range t.Constants {
		if k := c.Val().Kind(); k != constant.String && k != constant.Int {
			return fmt.Errorf("census: constant %s has kind %s, want String or Int", c.Name(), k)
		}
		e.declared[c.Val().ExactString()] = c.Val()
	}
	u.enums[e.name] = e
	u.all = append(u.all, e)
	return nil
}

// addSum records the variants of the sealed sum t, each with the value of its
// tag, which no field holds. The tag must be one of the package's
// enumerations, which is where a report lists its values.
func (u *Universe) addSum(t jsonshape.Type) error {
	pkg := t.Named.Obj().Pkg()
	spelled := types.TypeString(t.Tag, types.RelativeTo(pkg))
	tag, isNamed := types.Unalias(t.Tag).(*types.Named)
	if !isNamed || tag.Obj().Pkg() != pkg {
		return fmt.Errorf("census: %s is tagged by %s, which is no type of its package", t.Named.Obj().Name(), spelled)
	}
	if _, isEnum := u.enums[tag.Obj().Name()]; !isEnum {
		return fmt.Errorf("census: %s is tagged by %s, which declares no constants", t.Named.Obj().Name(), spelled)
	}
	variants := map[variantKey]valueKey{}
	for _, v := range t.Variants {
		key := variantKey{name: v.Named.Obj().Name(), byPointer: v.ByPointer}
		variants[key] = valueKey{enum: tag.Obj().Name(), spelling: v.Tag.ExactString()}
	}
	u.sums[t.Named.Obj().Name()] = variants
	return nil
}

// declares reports whether the universe's package declares t, a named type,
// so that its values hold facts. The zero universe declares none.
func (u *Universe) declares(t reflect.Type) bool {
	return u.pkgPath != "" && t.PkgPath() == u.pkgPath
}

// compareValues orders two values of one enumeration: integers by magnitude,
// strings by their text.
func compareValues(a, b constant.Value) int {
	if a.Kind() == constant.String {
		return strings.Compare(constant.StringVal(a), constant.StringVal(b))
	}
	return constant.Sign(constant.BinaryOp(a, token.SUB, b))
}
