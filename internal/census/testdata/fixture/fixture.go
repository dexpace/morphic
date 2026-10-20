// Package fixture declares the types a census test derives universes from:
// one root reaching every kind of member and value a census counts, and roots
// whose declarations a census refuses.
package fixture

import (
	"reflect"
	"time"
)

// IRVersion is the version a census build checks the declarations against.
const IRVersion = "1.2.3"

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

// Kind tags a Shape; no variant has KindHexagon.
type Kind string

// Kinds.
const (
	KindCircle  Kind = "circle"
	KindSquare  Kind = "square"
	KindHexagon Kind = "hexagon"
)

// Shape is a sealed sum holding a Circle through a pointer and a Square by
// value.
type Shape interface {
	shape()
	Kind() Kind
}

// Circle is a variant of Shape.
type Circle struct {
	Radius int `json:"radius,omitzero"`
}

func (*Circle) shape() {}

// Kind reports the variant's tag.
func (*Circle) Kind() Kind { return KindCircle }

// Square is a variant of Shape.
type Square struct {
	Side int `json:"side,omitzero"`
}

func (Square) shape() {}

// Kind reports the variant's tag.
func (Square) Kind() Kind { return KindSquare }

// Common is embedded in Root, and its members are its own.
type Common struct {
	ID string `json:"id,omitempty"`
}

// Leaf is reached through a pointer, by value, in a slice, as a map's value
// and as an embedding.
type Leaf struct {
	Label string `json:"label,omitempty"`
}

// Root reaches a member of every shape a census decides about.
type Root struct {
	Name     string           `json:"name,omitempty"`
	Untagged int              // no tag: the Go name
	Flag     bool             `json:"flag,omitzero"`
	Color    Color            `json:"color,omitempty"`
	Level    *Level           `json:"level,omitzero"`
	Leaf     *Leaf            `json:"leaf,omitzero"`
	Inline   Leaf             `json:"inline,omitzero"`
	Leaves   []Leaf           `json:"leaves,omitempty"`
	Grants   []string         `json:"grants,omitzero"`
	Index    map[Color]Leaf   `json:"index,omitzero"`
	Labels   map[string]Level `json:"labels,omitempty"`
	Sum      Shape            `json:"sum,omitzero"`
	Shapes   []Shape          `json:"shapes,omitempty"`
	Timeout  time.Duration    `json:"timeout,omitzero"`
	Reflect  reflect.Kind     `json:"reflect,omitzero"` // named Kind, as an enumeration here is
	Spread   *Leaf            `json:",embed"`
	Common
}

// Hidden has a field the encoder does not write.
type Hidden struct {
	Shown  string `json:"shown"`
	hidden string
}

// Skipped has a field tagged "-".
type Skipped struct {
	Shown   string `json:"shown"`
	Dropped string `json:"-"`
}

// Ratio is a floating-point type with a constant.
type Ratio float64

// Half is a value of Ratio, which no census can count.
const Half Ratio = 0.5

// Scaled holds a Ratio.
type Scaled struct {
	R Ratio `json:"r"`
}

// Loose holds a sum tagged by a type of no name.
type Loose struct {
	V Plain `json:"v"`
}

// Plain is a sealed sum whose Kind returns a string.
type Plain interface {
	plain()
	Kind() string
}

// PlainA is Plain's variant.
type PlainA struct{}

func (PlainA) plain() {}

// Kind reports the variant's tag.
func (PlainA) Kind() string { return "a" }

// Bare holds a sum tagged by a type that declares no constants.
type Bare struct {
	V Unlisted `json:"v"`
}

// Tagless is a named type with no constants.
type Tagless string

// Unlisted is a sealed sum tagged by Tagless.
type Unlisted interface {
	unlisted()
	Kind() Tagless
}

// UnlistedA is Unlisted's variant.
type UnlistedA struct{}

func (UnlistedA) unlisted() {}

// Kind reports the variant's tag.
func (UnlistedA) Kind() Tagless { return "a" }

// Monthly holds a sum tagged by a type of another package.
type Monthly struct {
	V Dated `json:"v"`
}

// Dated is a sealed sum tagged by time.Month.
type Dated interface {
	dated()
	Kind() time.Month
}

// DatedA is Dated's variant.
type DatedA struct{}

func (DatedA) dated() {}

// Kind reports the variant's tag.
func (DatedA) Kind() time.Month { return time.January }
