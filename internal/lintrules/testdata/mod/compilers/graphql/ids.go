package graphql

import (
	"github.com/dexpace/morphic/compilers/compile"
	frame "github.com/dexpace/morphic/internal/lintrules/testdata/mod/compilers/compile"
	"github.com/dexpace/morphic/ir"
	irx "github.com/dexpace/morphic/ir"
)

const anyTypeID ir.TypeID = "t/graphql/any" // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"

const aliasedID irx.OpID = "o/aliased" // want "idderivations: spells an ir.OpID as a constant outside compilers/compile; derive the ID through the framework"

const untyped = "t/graphql/untyped"

const gluedConst ir.TypeID = "t/" + "graphql" // want "idderivations: glues an ir.TypeID together outside compilers/compile; derive the ID through the framework"

const fixedSpace compile.Space = "graphql"

const untypedSpace = "graphql"

const gluedSpace compile.Space = "graph" + "ql" // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"

var keptA, keptB compile.Space = `alpha`, "beta"

var copied compile.Space = fixedSpace

// typeID names ir.TypeID otherwise.
type typeID = ir.TypeID

func lookupType(ir.TypeID) {}

// as converts to its type parameter, which an ID or compile.Space satisfies.
func as[T ~string](s string) T {
	return T(s) // want "idderivations: builds a value of the type parameter T, which an ir ID or a compile.Space satisfies, outside compilers/compile; derive the ID through the framework"
}

// glue joins two values of its type parameter.
func glue[T ~string](a, b T) T {
	return a + b // want "idderivations: builds a value of the type parameter T, which an ir ID or a compile.Space satisfies, outside compilers/compile; derive the ID through the framework"
}

// extend glues onto a value of its type parameter in place.
func extend[T ~string](t T) T {
	t += "/x" // want "idderivations: builds a value of the type parameter T, which an ir ID or a compile.Space satisfies, outside compilers/compile; derive the ID through the framework"
	return t
}

// spell spells a value of its type parameter as a constant.
func spell[T ~string]() T {
	return "t/graphql/generic" // want "idderivations: builds a value of the type parameter T, which an ir ID or a compile.Space satisfies, outside compilers/compile; derive the ID through the framework"
}

// opID converts to a type parameter only an ID satisfies.
func opID[T interface{ ir.OpID }](s string) T {
	return T(s) // want "idderivations: builds a value of the type parameter T, which an ir ID or a compile.Space satisfies, outside compilers/compile; derive the ID through the framework"
}

// namespace converts to a type parameter only compile.Space satisfies.
func namespace[S interface{ compile.Space }](s string) S {
	return S(s) // want "idderivations: builds a value of the type parameter S, which an ir ID or a compile.Space satisfies, outside compilers/compile; derive the ID through the framework"
}

// blank, same and number build nothing an ID can be: an empty constant, a
// value already of the type parameter, and one no ID satisfies.
func blank[T ~string]() T    { return "" }
func same[T ~string](t T) T  { return T(t) }
func number[N ~int](i int) N { return N(i) }

func plantIDs(doc *ir.Document, pointer string, existing ir.OpID, s compile.Space) ir.TypeID {
	_ = ir.TypeID("t/graphql" + pointer)        // want "idderivations: converts a value to ir.TypeID outside compilers/compile; derive the ID through the framework"
	_ = ir.PropID(pointer)                      // want "idderivations: converts a value to ir.PropID outside compilers/compile; derive the ID through the framework"
	_ = irx.EnumMemberID(pointer)               // want "idderivations: converts a value to ir.EnumMemberID outside compilers/compile; derive the ID through the framework"
	_ = ir.OpID(existing)                       // want "idderivations: converts a value to ir.OpID outside compilers/compile; derive the ID through the framework"
	_ = typeID(pointer)                         // want "idderivations: converts a value to ir.TypeID outside compilers/compile; derive the ID through the framework"
	_ = ir.TypeRef{Target: "t/graphql/ref"}     // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	_ = doc.Types["t/graphql/key"]              // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	lookupType("t/graphql/arg")                 // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	var fromUntyped ir.TypeID = untyped         // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	var fromFramework ir.TypeID = frame.Untyped // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	var aliasedType typeID = "t/graphql/a"      // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	parent := fromUntyped + "/items"            // want "idderivations: glues an ir.TypeID together outside compilers/compile; derive the ID through the framework"
	parent += "/0"                              // want "idderivations: glues an ir.TypeID together outside compilers/compile; derive the ID through the framework"
	switch parent {
	case "t/graphql/case": // want "idderivations: spells an ir.TypeID as a constant outside compilers/compile; derive the ID through the framework"
	}
	_ = compile.Space(pointer)                             // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
	_ = compile.Space('A')                                 // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
	_ = compile.Space(rune(65))                            // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
	_ = compile.OpID(compile.Space("graphql"+pointer), "") // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
	_ = compile.OpID(untypedSpace, pointer)                // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
	_ = compile.OpID("graph"+"ql", pointer)                // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"
	s += "/x"                                              // want "idderivations: builds a compile.Space from something other than one string literal; a namespace is a literal, or the path glued onto it loses its separator"

	_ = compile.OpID(fixedSpace, pointer)
	_ = compile.OpID(compile.Space("graphql"), pointer)
	_ = compile.OpID("graphql", pointer)
	_ = compile.OpID(("graphql"), pointer)
	_ = compile.Space(("graphql"))
	_ = compile.OpID(s, pointer)
	_ = as[ir.TypeID](pointer)
	if existing == "" || doc.Types[anyTypeID] == nil || doc.Types[frame.AnyID] == nil {
		return ""
	}
	_, _, _ = keptA, keptB, copied
	_, _, _, _ = aliasedID, gluedConst, gluedSpace, fromFramework
	return aliasedType
}
