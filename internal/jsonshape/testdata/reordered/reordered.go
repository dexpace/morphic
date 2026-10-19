// Package reordered declares the types of package good again, changing only
// what the JSON does not carry: declaration order, how a type is spelled, the
// name of every field and every string or boolean constant the JSON does not
// name, and the names of parameters and receivers.
package reordered

import "encoding/json/jsontext"

// Unreached is declared but nothing reaches it.
type Unreached struct {
	Y string `json:"x"`
}

// Toggle is a boolean enum, declared true first and once each.
type Toggle bool

// The toggles, renamed.
const (
	Yes Toggle = true
	No  Toggle = false
)

// Half is the one scale, declared before its type.
const Half Scale = 1.0 / 2

// Scale is a float enum.
type Scale float64

// Level is an integer enum, whose names say what its numbers mean.
type Level int

// The levels, under the names package good gives them.
const (
	Floor Level = 2
	High  Level = 10
	Low   Level = 2
)

// Sum is a sealed sum.
type Sum interface {
	Kind() Color
	sum()
}

// ByPointer is a variant held by pointer.
type ByPointer struct {
	Pointed string `json:"p"`
}

// Kind returns a typed constant this time.
func (*ByPointer) Kind() Color { return Azure }

func (*ByPointer) sum() {}

// ByValue is a variant held by value.
type ByValue struct {
	Valued string `json:"v"`
}

// MarshalJSON is a codec method a T has.
func (v ByValue) MarshalJSON() (out []byte, err error) { return nil, nil }

// Kind returns an untyped constant this time.
func (ByValue) Kind() Color { return "red" }

func (ByValue) sum() {}

// Color is a string enum.
type Color string

// The colors, renamed.
const (
	Azure   Color = "blue"
	Crimson Color = "red"
)

// Registry is a named map.
type Registry map[Color]Sum

// UnmarshalJSONFrom is a codec method only *Registry has.
func (reg *Registry) UnmarshalJSONFrom(dec *jsontext.Decoder) error { return nil }

// Spread is spelled here without an alias.
type Spread struct {
	Count byte `json:"item"`
}

// Tagged is embedded under a JSON name.
type Tagged struct {
	Below string `json:"under"`
}

// inner is embedded although it is unexported.
type inner struct {
	Down string `json:"deep"`
}

// Extra is embedded through a pointer.
type Extra struct {
	Further string `json:"more"`
}

// Base is embedded by value.
type Base struct {
	Retained string `json:"kept"`
}

// IsZero is the method omitzero asks.
func (b Base) IsZero() bool { return false }

// Root reaches every member, type and method form, in another order.
type Root struct {
	Size    Scale               `json:"scale"`
	Flip    Toggle              `json:"toggle"`
	Tiers   map[Level][]float64 `json:"levels"`
	Table   Registry            `json:"reg"`
	Nested  []Root              `json:"self"`
	Choice  Sum                 `json:"sum"`
	Where   jsontext.Pointer    `json:"pointer"`
	Direct  Spread              `json:"aliased"`
	ByColor map[Color]*Spread   `json:"index"`
	Matrix  [2][]uint8          `json:"grid"`

	Different Spread `json:",embed"`
	Tagged    `json:"tagged"`
	inner
	*Extra
	Base

	secret    string
	Dropped   string `json:"-"`
	Defaulted int    `json:",omitempty"`
	Untagged  bool
	Opts      *int   `json:"options,omitzero,string"`
	Label     string `json:"named"`
}
