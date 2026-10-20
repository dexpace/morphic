// Package planted holds what the depguard and ruleguard rules exist to refuse,
// one finding to a line, each line marked with the message it must draw.
// scripts/lint-planted.sh requires exactly those findings.
package planted

import (
	"encoding/json" // want "import 'encoding/json' is not allowed from list 'json-v1': use encoding/json/v2 and encoding/json/jsontext (depguard)"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"iter"
	"maps"

	"github.com/dexpace/morphic/ir"
	irx "github.com/dexpace/morphic/ir"
)

var (
	_ = json.Valid
	_ = jsontext.Value(nil)
	_ = jsonv2.Marshal
)

// lookalike has a registry's underlying type without being one, so building or
// writing it draws nothing.
type lookalike map[ir.TypeID]ir.TypeDef

func plant(doc *ir.Document, reg ir.TypeRegistry, other lookalike, defs []ir.TypeDef, seq iter.Seq[ir.TypeDef], byDef map[ir.TypeDef]int) {
	built := ir.TypeRegistry{}          // want "ruleguard: builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	aliased := irx.TypeRegistry{}       // want "ruleguard: builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	made := make(ir.TypeRegistry)       // want "ruleguard: builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	sized := make(ir.TypeRegistry, 1)   // want "ruleguard: builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	converted := ir.TypeRegistry(other) // want "ruleguard: builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	cloned := maps.Clone(reg)           // want "ruleguard: builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"

	doc.Types["a"] = nil // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	held := doc.Types
	held["b"] = nil                                                        // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	other["c"], built["c"] = nil, nil                                      // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	other["g"], other["g"], built["g"] = nil, nil, nil                     // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	delete(doc.Types, "d")                                                 // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	clear(reg)                                                             // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	maps.Copy(reg, built)                                                  // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	maps.Insert(reg, maps.All(built))                                      // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	maps.DeleteFunc(reg, func(ir.TypeID, ir.TypeDef) bool { return true }) // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	for reg["h"] = range seq {                                             // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	}
	for reg["i"], _ = range byDef { // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	}
	for _, reg["j"] = range defs { // want "ruleguard: writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types (gocritic)"
	}
	for _, other["k"] = range defs {
	}

	other["e"] = nil
	delete(other, "f")
	_ = lookalike{}
	_ = maps.Clone(other)
	_, _, _, _, _ = aliased, made, sized, converted, cloned
}
