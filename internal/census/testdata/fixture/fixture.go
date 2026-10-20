// Package fixture declares the types a census test derives a universe from:
// one root reaching every kind of field the derivation crosses, and the
// enumerations beside it.
package fixture

import "time"

// Color is a string enumeration; Crimson repeats Red's value.
type Color string

// Colors.
const (
	Red     Color = "red"
	Crimson Color = "red"
	Blue    Color = "blue"
)

// Level is an integer enumeration of values that differ in sign and length.
type Level int

// Levels.
const (
	Lowest  Level = -20
	Low     Level = -1
	Zero    Level = 0
	High    Level = 10
	Highest Level = 100
)

// Bits is a set of single bits.
type Bits uint8

// Bits, as 1 << iota.
const (
	BitA Bits = 1 << iota
	BitB
	BitC
)

// Unreached is an enumeration no root reaches.
type Unreached string

// Never is the one value of Unreached.
const Never Unreached = "never"

// Constants no enumeration holds: one of another package's type, one untyped
// and one of a basic type.
const (
	Timeout time.Duration = 5
	Plain                 = 3
	Typed   int           = 4
)

// Shape is a sealed sum; Circle and Square are its variants.
type Shape interface{ shape() }

// Circle is a variant of Shape, held by pointer.
type Circle struct {
	Radius int `json:"radius"`
}

func (*Circle) shape() {}

// Square is a variant of Shape, held by value.
type Square struct {
	Side int `json:"side"`
}

func (Square) shape() {}

// Leaf is a struct reached through a map of slices of pointers.
type Leaf struct {
	Label string `json:"label"`
}

// InArray is a struct reached only as the element of an array.
type InArray struct {
	A string `json:"a"`
}

// InSlice is a struct reached only as the element of a slice.
type InSlice struct {
	S string `json:"s"`
}

// InPointer is a struct reached only through a pointer.
type InPointer struct {
	P string `json:"p"`
}

// InMapValue is a struct reached only as the value of a map.
type InMapValue struct {
	V string `json:"v"`
}

// InMapKey is a struct reached only as the key of a map.
type InMapKey struct {
	K string `json:"k"`
}

// Common is embedded in Root, and its members are its own.
type Common struct {
	ID string `json:"id"`
}

// Pointed is embedded in Root by pointer.
type Pointed struct {
	P bool `json:"p"`
}

// Renamed is embedded in Root under a name, which makes it a member.
type Renamed struct {
	R int `json:"r"`
}

// Root reaches a struct through every container, an interface, an embedded
// struct and a struct of another package, and has a field of every shape the
// derivation decides about.
type Root struct {
	Name     string               `json:"name"`
	Untagged int                  // no tag: the Go name
	Options  string               `json:",omitempty"`
	Skipped  string               `json:"-"`
	hidden   string               // unexported
	Color    Color                `json:"color"`
	Level    *Level               `json:"level,omitzero"`
	Bits     []Bits               `json:"bits"`
	Pair     [2]InArray           `json:"pair"`
	Sliced   []InSlice            `json:"sliced"`
	Ptr      *InPointer           `json:"ptr"`
	Values   map[Level]InMapValue `json:"values"`
	Keyed    map[InMapKey]Color   `json:"keyed"`
	Nested   map[string][]*Leaf   `json:"nested"`
	Sum      Shape                `json:"sum"`
	When     time.Time            `json:"when"`
	Inline   struct{ X Leaf }     `json:"inline"`
	Common
	*Pointed
	Renamed `json:"renamed"`
}

// Opaque holds an interface no one said how to cross.
type Opaque struct {
	V any `json:"v"`
}
