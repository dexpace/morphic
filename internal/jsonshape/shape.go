package jsonshape

import (
	"fmt"
	"go/constant"
	"go/types"
	"slices"
	"strconv"
	"strings"
)

// Shape is the JSON shape of the named types one root reaches, as Read finds
// it: the parts a description spells, for a reader that needs them as data.
type Shape struct {
	// Types holds one entry per named type the root reaches, sorted by the
	// name a description spells for it.
	Types []Type

	pkg  *types.Package
	root string
}

// Type is one named type's part of a shape.
type Type struct {
	// Named is the type's declaration.
	Named *types.Named
	// Members are the fields of a struct that the encoder reads, in
	// declaration order; nil for any other type.
	Members []Member
	// Tag is the type a sealed sum's Kind method returns, and Variants are the
	// types the sum holds, sorted by the tag's spelling; both are zero for any
	// other type.
	Tag      types.Type
	Variants []Variant
	// Constants are the constants of this type that its package declares, in
	// value order, one per name.
	Constants []*types.Const

	// name spells the type: bare when the described package declares it,
	// qualified by import path otherwise. head is what it is defined as:
	// struct, interface, or a type expression such as string or map[K]V. tag
	// spells Tag.
	name, head, tag string
	// codecs are the lines of the methods the encoder calls.
	codecs []string
}

// Member is one struct field the encoder reads.
type Member struct {
	// Field is the struct field, at Index among the struct's fields.
	Field *types.Var
	Index int
	// Name is the member's JSON name, and is empty for an embedding, whose
	// members the encoder writes as the struct's own.
	Name     string
	Embedded bool
	// Options are the json tag's options, sorted, without "embed".
	Options []string

	// typ spells the field's type.
	typ string
}

// Variant is one type a sealed sum holds, through a pointer when ByPointer
// is set, with the constant its Kind method returns.
type Variant struct {
	Named     *types.Named
	ByPointer bool
	Tag       constant.Value

	// spelled is the variant's type as a description spells it.
	spelled string
}

// String spells the shape as Describe does: a header naming the package, not
// its import path, which is no part of the shape, so moving the module moves
// no pin; then each type with its lines.
func (s *Shape) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "jsonshape %d: %s.%s\n", Format, s.pkg.Name(), s.root)
	for _, t := range s.Types {
		fmt.Fprintf(&b, "\ntype %s %s\n", t.name, t.head)
		for _, line := range t.lines() {
			fmt.Fprintf(&b, "\t%s\n", line)
		}
	}
	return b.String()
}

// lines are a type's tag and variants, its embeddings and members, its
// methods and its constants, each group sorted, so a reordered declaration
// describes the same shape.
func (t *Type) lines() []string {
	var out []string
	if t.Tag != nil {
		out = append(out, "tag "+t.tag)
	}
	for _, v := range t.Variants {
		out = append(out, v.line())
	}

	var embeds, fields []string
	for _, m := range t.Members {
		if m.Embedded {
			embeds = append(embeds, m.line())
		} else {
			fields = append(fields, m.line())
		}
	}
	slices.Sort(embeds)
	slices.Sort(fields)
	out = append(append(append(out, embeds...), fields...), t.codecs...)

	consts := make([]string, len(t.Constants))
	for i, c := range t.Constants {
		consts[i] = constLine(c)
	}
	return append(out, slices.Compact(consts)...)
}

// line spells one member: "embed" or its quoted JSON name, then its type and
// its options.
func (m Member) line() string {
	head := strconv.Quote(m.Name)
	if m.Embedded {
		head = "embed"
	}
	return strings.Join(append([]string{head, m.typ}, m.Options...), " ")
}

// line spells one variant: its tag, then the type the sum holds.
func (v Variant) line() string {
	return "variant " + v.Tag.ExactString() + " " + v.spelled
}

// constLine spells one constant. A string or a boolean says in the JSON what
// it means, so it is spelled by value alone and listed once however many names
// it has. A number says nothing of what it means; its constant's name does, so
// a numeric constant is spelled with its name, and reordering iota flags
// changes the description.
func constLine(c *types.Const) string {
	v := c.Val()
	if v.Kind() == constant.Int || v.Kind() == constant.Float {
		return fmt.Sprintf("const %s = %s", c.Name(), v.ExactString())
	}
	return "const " + v.ExactString()
}
