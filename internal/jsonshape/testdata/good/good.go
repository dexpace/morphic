// Package good declares one root that reaches every construct a description
// spells, and a type nothing reaches.
package good

import "encoding/json/jsontext"

// Root reaches every member, type and method form.
type Root struct {
	Named     string `json:"named"`
	Options   *int   `json:"options,string,omitzero"`
	Untagged  bool
	Defaulted int    `json:",omitempty"`
	Ignored   string `json:"-"`
	hidden    string

	Base
	*Extra
	inner
	Tagged   `json:"tagged"`
	Explicit Spread `json:",embed"`

	Grid    [2][]byte           `json:"grid"`
	Index   map[Color]*Spread   `json:"index"`
	Aliased Alias               `json:"aliased"`
	Pointer jsontext.Pointer    `json:"pointer"`
	Sum     Sum                 `json:"sum"`
	Self    []Root              `json:"self"`
	Reg     Registry            `json:"reg"`
	Levels  map[Level][]float64 `json:"levels"`
	Toggle  Toggle              `json:"toggle"`
	Scale   Scale               `json:"scale"`
}

// Base is embedded by value, and gives Root a promoted IsZero.
type Base struct {
	Kept string `json:"kept"`
}

// IsZero is the method omitzero asks.
func (Base) IsZero() bool { return false }

// Extra is embedded through a pointer.
type Extra struct {
	More string `json:"more"`
}

// inner is embedded although it is unexported, as the encoder does.
type inner struct {
	Deep string `json:"deep"`
}

// Tagged is embedded under a JSON name, which makes it a member.
type Tagged struct {
	Under string `json:"under"`
}

// Spread is embedded by the embed option, and is otherwise a member's type.
type Spread struct {
	Item uint8 `json:"item"`
}

// Alias names Spread, so a member declared through it is a Spread.
type Alias = Spread

// Color is a string enum, declared out of order.
type Color string

// The colors, one of them under two names.
const (
	Red     Color = "red"
	Blue    Color = "blue"
	Scarlet Color = "red"
)

// Plain is untyped, so it is no Color.
const Plain = "plain"

// Level is an integer enum whose order is numeric, not lexical. Its names say
// what each number means, so each is described, two of them on one number.
type Level int

// The levels.
const (
	High  Level = 10
	Low   Level = 2
	Floor Level = 2
)

// Scale is a float enum, as much a number as Level.
type Scale float64

// Half is the one scale.
const Half Scale = 0.5

// Toggle is a boolean enum, whose values have an order but not a less-than.
type Toggle bool

// The toggles, one of them under two names.
const (
	On   Toggle = true
	Off  Toggle = false
	Also Toggle = true
)

// Registry is a named map with a decoder of its own.
type Registry map[Color]Sum

// UnmarshalJSONFrom is a codec method only *Registry has.
func (r *Registry) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }

// String is not a method the encoder calls.
func (r Registry) String() string { return "" }

// Sum is a sealed sum.
type Sum interface {
	sum()
	Kind() Color
}

// ByValue is a variant held by value.
type ByValue struct {
	V string `json:"v"`
}

func (ByValue) sum() {}

// Kind returns a typed constant.
func (ByValue) Kind() Color { return Red }

// MarshalJSON is a codec method a T has.
func (ByValue) MarshalJSON() ([]byte, error) { return nil, nil }

// ByPointer is a variant held by pointer.
type ByPointer struct {
	P string `json:"p"`
}

func (*ByPointer) sum() {}

// Kind returns an untyped constant.
func (*ByPointer) Kind() Color { return "blue" }

// Held names a variant again, which is still listed once.
type Held = ByValue

// Partial has Kind but not the marker, so it is no variant.
type Partial struct{}

// Kind would be a variant's.
func (Partial) Kind() Color { return Red }

// Other is an interface, which a variant is not.
type Other interface {
	sum()
	Kind() Color
}

// Unreached is declared but nothing reaches it.
type Unreached struct {
	X string `json:"x"`
}
