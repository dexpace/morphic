// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// settlement is what the Settle methods of the package's compile.Settlements
// may write: each one's registry parameter, while Settle leaves it bound to
// the registry it was handed, outside the function literals in its body, which
// can run after Settle returns and so past the node count compile.Types checks.
type settlement struct {
	// params maps each registry parameter to whether Settle leaves it bound.
	params   map[types.Object]bool
	closures []*ast.FuncLit
}

// inClosure reports whether pos is inside one of the function literals.
func (s settlement) inClosure(pos token.Pos) bool {
	return slices.ContainsFunc(s.closures, func(lit *ast.FuncLit) bool {
		return lit.Pos() <= pos && pos < lit.End()
	})
}

// settlements returns what the Settle methods the package declares, on types
// that are compile.Settlements, may write.
func settlements(pass *analysis.Pass) settlement {
	s := settlement{params: map[types.Object]bool{}}
	tn := typeNamed(pass.Pkg, compilePath, "Settlement")
	if tn == nil {
		return s
	}
	iface, _ := tn.Type().Underlying().(*types.Interface)
	for _, f := range productionFiles(pass) {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Settle" {
				continue
			}
			fn, isFunc := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !isFunc || iface == nil || !types.Implements(fn.Signature().Recv().Type(), iface) {
				continue
			}
			param := fn.Signature().Params().At(0)
			s.params[param] = !rebinds(pass.TypesInfo, fd.Body, param)
			for n := range ast.Preorder(fd.Body) {
				if lit, isLit := n.(*ast.FuncLit); isLit {
					s.closures = append(s.closures, lit)
				}
			}
		}
	}
	return s
}

// rebinds reports whether body assigns param, ranges into it or takes its
// address: after any of those, what param names may be another registry.
func rebinds(info *types.Info, body *ast.BlockStmt, param types.Object) bool {
	names := func(e ast.Expr) bool {
		id, _ := ast.Unparen(e).(*ast.Ident)
		return info.Uses[id] == param
	}
	for n := range ast.Preorder(body) {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if slices.ContainsFunc(n.Lhs, names) {
				return true
			}
		case *ast.RangeStmt:
			if names(n.Key) || names(n.Value) {
				return true
			}
		case *ast.UnaryExpr: // & is the one unary operator a map takes
			if names(n.X) {
				return true
			}
		default:
		}
	}
	return false
}
