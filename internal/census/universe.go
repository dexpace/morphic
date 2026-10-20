package census

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/dexpace/morphic/internal/decls"
	"github.com/dexpace/morphic/ir"
)

// maxTypes bounds the types one derivation visits. A package declares finitely
// many, so reaching it means the walk went wrong, not that the schema is large.
const maxTypes = 4096

// member is one struct member the census counts.
type member struct {
	// index is the member's position among its struct's fields.
	index int
	// name is "<GoStructName>.<jsonName>".
	name string
}

// Universe is every fact a census counts, derived from the declarations of
// the IR and never listed by hand: the members of its structs and the values
// of its enumerations. The zero value holds none.
type Universe struct {
	structs map[reflect.Type][]member
	members []string
	enums   map[reflect.Type]*enumeration
	all     []*enumeration
}

// NewUniverse derives the universe from package ir. It reads the package's
// constants through go/packages, which resolves the import path from the
// working directory, so it fails when run outside the module.
func NewUniverse() (Universe, error) {
	root := reflect.TypeFor[ir.Document]()
	p, err := decls.Load(root.PkgPath())
	if err != nil {
		return Universe{}, fmt.Errorf("census: %w", err)
	}
	// TypeKind is reached through TypeDef.Kind rather than through a field, so
	// it is a root of its own.
	return derive([]reflect.Type{root, reflect.TypeFor[ir.TypeKind]()}, p, expandTypeDef)
}

// expandFunc names the concrete types each interface the roots reach may hold,
// given the values every enumeration of the package declares, sorted.
type expandFunc func(declared map[string][]string) (map[reflect.Type][]reflect.Type, error)

// derive builds the universe of the types the roots reach in the package p
// declares, which is the package of the first root. The interfaces expand
// names are the only ones it will cross. An enumeration no root reaches is no
// part of what a document holds, so it is left out.
func derive(roots []reflect.Type, p *decls.Package, expand expandFunc) (Universe, error) {
	if len(roots) == 0 {
		return Universe{}, errors.New("census: no root type to derive a universe from")
	}
	enums, err := enumerations(p)
	if err != nil {
		return Universe{}, err
	}
	declared := make(map[string][]string, len(enums))
	for _, e := range enums {
		declared[e.name] = slices.Sorted(maps.Keys(e.declared))
	}
	concrete, err := expand(declared)
	if err != nil {
		return Universe{}, err
	}
	r, err := reachable(roots, concrete)
	if err != nil {
		return Universe{}, err
	}

	u := Universe{structs: r.structs, enums: map[reflect.Type]*enumeration{}}
	for _, e := range enums {
		if t, reached := r.named[e.name]; reached {
			u.enums[t] = e
			u.all = append(u.all, e)
		}
	}
	for _, fields := range r.structs {
		for _, f := range fields {
			u.members = append(u.members, f.name)
		}
	}
	slices.Sort(u.members)
	return u, nil
}

// expandTypeDef maps the TypeDef interface to the type of every kind the IR
// declares, which is how a sealed sum is crossed: through the registry that
// decoding dispatches on.
func expandTypeDef(declared map[string][]string) (map[reflect.Type][]reflect.Type, error) {
	spellings, found := declared["TypeKind"]
	if !found {
		return nil, errors.New("census: the IR declares no TypeKind constants")
	}

	var kinds []reflect.Type
	for _, kind := range spellings {
		td, registered := ir.NewTypeDef(ir.TypeKind(kind))
		if !registered {
			return nil, fmt.Errorf("census: TypeKind %q has no registered TypeDef", kind)
		}
		kinds = append(kinds, reflect.TypeOf(td))
	}
	return map[reflect.Type][]reflect.Type{reflect.TypeFor[ir.TypeDef](): kinds}, nil
}

// reaching is the part of the type graph a walk from the roots found.
type reaching struct {
	pkgPath  string
	concrete map[reflect.Type][]reflect.Type
	structs  map[reflect.Type][]member
	// named holds the package's named types that are not structs, by name.
	named map[string]reflect.Type
}

// reachable walks the types the roots reach through pointers, slices, arrays,
// maps and struct fields, crossing an interface through concrete. The walk is a
// queue, and the number of types it visits is bounded. The package it describes
// is the first root's.
func reachable(roots []reflect.Type, concrete map[reflect.Type][]reflect.Type) (*reaching, error) {
	r := &reaching{
		pkgPath:  roots[0].PkgPath(),
		concrete: concrete,
		structs:  map[reflect.Type][]member{},
		named:    map[string]reflect.Type{},
	}
	seen := map[reflect.Type]bool{}
	for _, root := range roots {
		seen[root] = true
	}
	queue := slices.Clone(roots)
	for len(queue) > 0 {
		t := queue[0]
		queue = queue[1:]
		next, err := r.visit(t)
		if err != nil {
			return nil, err
		}
		for _, n := range next {
			if seen[n] {
				continue
			}
			if len(seen) == maxTypes {
				return nil, fmt.Errorf("census: %s reaches more than %d types", roots[0], maxTypes)
			}
			seen[n] = true
			queue = append(queue, n)
		}
	}
	return r, nil
}

// visit records t and returns the types it leads to.
func (r *reaching) visit(t reflect.Type) ([]reflect.Type, error) {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return []reflect.Type{t.Elem()}, nil
	case reflect.Map:
		return []reflect.Type{t.Key(), t.Elem()}, nil
	case reflect.Interface:
		impls, expands := r.concrete[t]
		if !expands {
			return nil, fmt.Errorf("census: interface %s has no known implementations", t)
		}
		return impls, nil
	case reflect.Struct:
		return r.structure(t), nil
	default:
		if t.PkgPath() == r.pkgPath {
			r.named[t.Name()] = t
		}
		return nil, nil
	}
}

// structure records a struct of the package with its members and returns the
// types of its fields. A struct the package does not declare, a time.Time or
// an unnamed struct, is not part of the surface it describes.
//
// A field is a member when the encoder writes it under a name of its own: an
// exported field not tagged "-". An embedded struct is crossed but is no
// member of the struct that embeds it; its fields are its own.
func (r *reaching) structure(t reflect.Type) []reflect.Type {
	if t.PkgPath() != r.pkgPath {
		return nil
	}
	members := []member{}
	var next []reflect.Type
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		switch {
		case f.Anonymous && name == "":
			next = append(next, f.Type)
		case f.IsExported():
			if name == "" {
				name = f.Name
			}
			members = append(members, member{index: i, name: t.Name() + "." + name})
			next = append(next, f.Type)
		}
	}
	r.structs[t] = members
	return next
}
