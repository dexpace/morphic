// Package graphql stands for a compiler other than the framework, which every
// rule holds. Each marked line draws the finding it names, and nothing else
// here draws one.
package graphql

import (
	"iter"
	"maps"

	owner "github.com/dexpace/morphic/internal/lintrules/testdata/mod/ir"
	"github.com/dexpace/morphic/ir"
	irx "github.com/dexpace/morphic/ir"
)

// unnamed is a registry in all but name.
type unnamed = map[ir.TypeID]ir.TypeDef

// lookalike is one under another name.
type lookalike map[ir.TypeID]ir.TypeDef

// holder keeps a registry in a field.
type holder struct{ reg ir.TypeRegistry }

// put writes any map it is handed, which a rule reading the call cannot see.
func put[M ~map[K]V, K comparable, V any](m M, k K, v V) { m[k] = v }

// Insert reads a registry under the name of a function of package maps.
func Insert(reg ir.TypeRegistry, id ir.TypeID) bool { return reg[id] != nil }

// fresh returns a registry it built.
func fresh() map[ir.TypeID]ir.TypeDef {
	return map[ir.TypeID]ir.TypeDef{} // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
}

func plantRegistry(doc *ir.Document, reg ir.TypeRegistry, seq iter.Seq2[ir.TypeID, ir.TypeDef], vals iter.Seq[ir.TypeDef], defs []ir.TypeDef, id ir.TypeID, def ir.TypeDef) {
	_ = ir.TypeRegistry{}                 // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = irx.TypeRegistry{}                // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = make(ir.TypeRegistry)             // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = make(ir.TypeRegistry, 1)          // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = lookalike{}                       // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = unnamed{}                         // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = maps.Clone(reg)                   // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	doc.Types = maps.Collect(seq)         // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = holder{reg: unnamed{}}            // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	_ = ir.Document{Types: make(unnamed)} // want "registrywrites: builds a type registry (a map from ir.TypeID to ir.TypeDef) outside ir and compilers/compile; go through compile.Types"
	doc.Types = fresh()

	doc.Types[id] = def // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	held := doc.Types
	(held[id]) = def // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	held[id] = def   // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	var a, b, c int
	a, b, c, reg[id] = 1, 2, 3, def // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	var m unnamed = doc.Types
	m[id] = def // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	p := &doc.Types
	(*p)[id] = def                                                         // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	delete(doc.Types, id)                                                  // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	clear(reg)                                                             // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	maps.Copy(reg, doc.Types)                                              // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	maps.Insert(reg, maps.All(doc.Types))                                  // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	maps.DeleteFunc(reg, func(ir.TypeID, ir.TypeDef) bool { return true }) // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	for reg[id] = range vals {                                             // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	}
	for _, reg[id] = range defs { // want "registrywrites: writes a type registry outside ir and compilers/compile; go through compile.Types, or rewrite a node in a compile.Settlement"
	}
	put(reg, id, def)                                         // want "registrywrites: passes a type registry to a generic function, whose writes this rule cannot read; keep registry writes in ir and compilers/compile"
	put[ir.TypeRegistry](reg, id, def)                        // want "registrywrites: passes a type registry to a generic function, whose writes this rule cannot read; keep registry writes in ir and compilers/compile"
	put[ir.TypeRegistry, ir.TypeID, ir.TypeDef](reg, id, def) // want "registrywrites: passes a type registry to a generic function, whose writes this rule cannot read; keep registry writes in ir and compilers/compile"

	_ = ir.TypeRegistry(m)
	_ = doc.Types[id]
	_ = len(reg)
	_ = len(defs)
	_ = owner.Count(reg)
	for range maps.Keys(reg) {
	}
	other := map[ir.TypeID]int{}
	other[id] = a + b + c
	put(other, id, 1)
	_ = Insert(reg, id)
}
