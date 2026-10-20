package planted

import (
	cmp "github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
	irx "github.com/dexpace/morphic/ir"
)

const anyTypeID ir.TypeID = "t/graphql/any" // want "ruleguard: an ir ID is declared from a string literal outside compilers/compile; derive it through the framework (gocritic)"

const fixedSpace cmp.Space = "graphql"

var (
	prefix            = "a"
	glued   cmp.Space = cmp.Space(prefix) // want "ruleguard: a compile.Space is built from a non-literal; a namespace is a string literal, or the path glued onto it loses its separator (gocritic)"
	pending cmp.Space
)

func plantIDs(pointer string, existing ir.OpID, name string) []any {
	named := ir.TypeID("t/graphql" + pointer)       // want "ruleguard: an ir ID is converted from a string outside compilers/compile; derive it through the framework (gocritic)"
	prop := ir.PropID("p/graphql" + pointer)        // want "ruleguard: an ir ID is converted from a string outside compilers/compile; derive it through the framework (gocritic)"
	alias := irx.OpID(name)                         // want "ruleguard: an ir ID is converted from a string outside compilers/compile; derive it through the framework (gocritic)"
	member := ir.EnumMemberID("e/graphql" + name)   // want "ruleguard: an ir ID is converted from a string outside compilers/compile; derive it through the framework (gocritic)"
	lost := cmp.OpID(cmp.Space("graphql"+name), "") // want "ruleguard: a compile.Space is built from a non-literal; a namespace is a string literal, or the path glued onto it loses its separator (gocritic)"
	fromVar := cmp.Space(name)                      // want "ruleguard: a compile.Space is built from a non-literal; a namespace is a string literal, or the path glued onto it loses its separator (gocritic)"
	fromRune := cmp.Space(rune(65))                 // want "ruleguard: a compile.Space is built from a non-literal; a namespace is a string literal, or the path glued onto it loses its separator (gocritic)"
	op := cmp.OpID(fixedSpace, pointer)
	literal := cmp.OpID(cmp.Space("graphql"), name)
	var copied ir.OpID = existing
	return []any{anyTypeID, glued, pending, named, prop, alias, member, lost, fromVar, fromRune,
		op, literal, copied}
}
