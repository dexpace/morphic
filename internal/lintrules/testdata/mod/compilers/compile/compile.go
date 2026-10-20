// Package compile stands for the framework, where every rule's subject is
// allowed: it owns the registry and the grammars.
package compile

import (
	"net/url"

	"github.com/dexpace/morphic/ir"
)

// AnyID is an ID the framework spells, which a compiler may name.
const AnyID ir.TypeID = "t/compile/any"

// Untyped is a string the framework declares, which a compiler may not make an
// ID of.
const Untyped = "t/compile/untyped"

// Framework builds, writes, spells and names, which draws nothing but the
// fragment reading, which the framework is not excused.
func Framework(doc *ir.Document, name string) (ir.Naming, error) {
	reg := ir.TypeRegistry{}
	reg[ir.TypeID(name)] = nil
	reg["t/compile/any"] = nil
	doc.Types = reg
	_, err := url.QueryUnescape(name) // want "fragmentreadings: url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer"
	return ir.Naming{Source: name, Canonical: name, Hint: name}, err
}
