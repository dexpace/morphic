// Package bad declares roots that each reach one type a description refuses.
package bad

import "encoding/json/jsontext"

// Generic reaches an instantiation.
type Generic struct {
	G Box[int] `json:"g"`
}

// Box is generic.
type Box[T any] struct {
	V T `json:"v"`
}

// Anonymous reaches a struct with no name.
type Anonymous struct {
	A struct {
		X int `json:"x"`
	} `json:"a"`
}

// Dynamic reaches an unnamed interface.
type Dynamic struct {
	D any `json:"d"`
}

// Callback reaches a func.
type Callback struct {
	F func() `json:"f"`
}

// Complex reaches a number JSON cannot write.
type Complex struct {
	C complex128 `json:"c"`
}

// ComplexKey reaches one as a map key.
type ComplexKey struct {
	M map[complex128]string `json:"m"`
}

// Quoted spells its name in quotes.
type Quoted struct {
	Q string `json:"'q,r'"`
}

// EmptyOption has an empty tag option.
type EmptyOption struct {
	E string `json:"e,,omitempty"`
}

// Predeclared reaches a predeclared type.
type Predeclared struct {
	E error `json:"e"`
}

// Open reaches an interface any package can implement.
type Open struct {
	O Unsealed `json:"o"`
}

// Unsealed has only exported methods.
type Unsealed interface {
	Kind() Tag
}

// Tag is a sum's tag type.
type Tag string

// NoKind reaches a sealed sum without a Kind method.
type NoKind struct {
	N KindLess `json:"n"`
}

// KindLess is sealed but has no Kind.
type KindLess interface {
	kindLess()
}

// ArgKind reaches a sealed sum whose Kind takes an argument.
type ArgKind struct {
	A KindWithArg `json:"a"`
}

// KindWithArg has a Kind that is not a tag.
type KindWithArg interface {
	kindWithArg()
	Kind(int) Tag
}

// ShapelessTag reaches a sum whose tag type has no JSON shape.
type ShapelessTag struct {
	S FuncTagged `json:"s"`
}

// FuncTagged returns a func as its tag.
type FuncTagged interface {
	funcTagged()
	Kind() func()
}

// Lonely reaches a sealed sum nothing implements.
type Lonely struct {
	L Alone `json:"l"`
}

// Alone has no variant.
type Alone interface {
	alone()
	Kind() Tag
}

// Computed reaches a variant whose Kind is not a constant.
type Computed struct {
	C ComputedSum `json:"c"`
}

// ComputedSum has one variant.
type ComputedSum interface {
	computed()
	Kind() Tag
}

// FromField returns a field as its tag.
type FromField struct {
	K Tag `json:"k"`
}

func (FromField) computed() {}

// Kind returns a field.
func (f FromField) Kind() Tag { return f.K }

// TwoSteps reaches a variant whose Kind takes two statements.
type TwoSteps struct {
	T TwoStepSum `json:"t"`
}

// TwoStepSum has one variant.
type TwoStepSum interface {
	twoStep()
	Kind() Tag
}

// Stepped computes its tag first.
type Stepped struct{}

func (Stepped) twoStep() {}

// Kind assigns, then returns.
func (Stepped) Kind() Tag {
	tag := Tag("stepped")
	return tag
}

// Panicky reaches a variant whose Kind does not return.
type Panicky struct {
	P PanicSum `json:"p"`
}

// PanicSum has one variant.
type PanicSum interface {
	panicky()
	Kind() Tag
}

// Panics never returns a tag.
type Panics struct{}

func (Panics) panicky() {}

// Kind panics.
func (Panics) Kind() Tag { panic("no tag") }

// Foreign reaches a variant whose Kind another package declares.
type Foreign struct {
	F ForeignSum `json:"f"`
}

// ForeignSum dispatches on a token's kind.
type ForeignSum interface {
	foreign()
	Kind() jsontext.Kind
}

// Token takes its Kind from jsontext.Token.
type Token struct {
	jsontext.Token
}

func (Token) foreign() {}

// Unnamed is an alias of a type with no name.
type Unnamed = []string

// helper is a function, not a method.
func helper() {}

var _ = helper
