// Package fixture declares the shapes decls is tested against.
package fixture

// Color is a string enum.
type Color string

// Alias names Color through an alias declaration.
type Alias = Color

// Bytes is an alias to a type that is not named.
type Bytes = []byte

// Count is an integer enum.
type Count int

// Colors, each spelled differently.
const (
	Red   Color = "red"
	Green Alias = "gr" + "een"
	Blue        = Color("blue")
)

// One is a Count.
const One Count = 1

// Plain is an untyped string constant, of no named type.
const Plain = "plain"

// Huge is an untyped integer constant past what an int64 holds.
const Huge = 1 << 64

// Hello is a function, which declares no type.
func Hello() {}

// Method is Color's own.
func (Color) Method() {}

// Wrapper reaches Method only by embedding Color.
type Wrapper struct{ Color }

// Shade declares a Method of its own.
type Shade int

// Method is Shade's own.
func (*Shade) Method() {}
