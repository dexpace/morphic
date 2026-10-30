package lintrules

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// serviceTreeWrites refuses building or writing a slice of the service tree,
// of ir.Service, ir.OperationGroup or ir.Operation, outside ir and the
// compilers; any other package edits one through an ir.Editor.
//
// A write lands in a slice's array: to or through an element, append, copy,
// clear, a slices function reordering or removing, sort.Slice, an element's
// address, and a module generic over a tree slice or element. As nothing else
// builds one, each slice met is a document's. A write through what an element
// holds by reference, by reflect, or in a generic outside slices and the
// module is out of its sight.
func (l layout) serviceTreeWrites() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "servicetreewrites",
		Doc:  "refuse building or writing a document's services, groups and operations outside ir and the compilers",
		Run:  l.runServiceTreeWrites,
	}
}

const (
	treeBuiltMessage   = "builds a slice of the service tree (of ir.Service, ir.OperationGroup or ir.Operation) outside ir and the compilers; read a document's and edit it through an ir.Editor"
	treeWrittenMessage = "writes a slice of the service tree outside ir and the compilers; edit a document's services, groups and operations through an ir.Editor"
	treeAddressMessage = "takes the address of an element of the service tree outside ir and the compilers, through which it can write a document; read the element by value and edit it through an ir.Editor"
	treeGenericMessage = "instantiates a generic with a slice or an element of the service tree, whose writes this rule cannot read; keep service tree writes in ir and the compilers"
)

func (l layout) runServiceTreeWrites(pass *analysis.Pass) (any, error) {
	path := pass.Pkg.Path()
	if !l.inModule(path) || l.within(path, "ir") || l.within(path, "compilers") {
		return nil, nil
	}
	c := treeCheck{l: l, pass: pass}
	for _, name := range []string{"Service", "OperationGroup", "Operation"} {
		tn := typeNamed(pass.Pkg, irPath, name)
		if tn == nil {
			return nil, nil // a package that reaches no ir holds none of its trees
		}
		c.elems = append(c.elems, tn.Type())
	}
	for _, f := range productionFiles(pass) {
		for n := range ast.Preorder(f) {
			c.node(n)
		}
	}
	return nil, nil
}

// treeCheck finds the service tree builds and writes in one package.
type treeCheck struct {
	l    layout
	pass *analysis.Pass
	// elems are ir.Service, ir.OperationGroup and ir.Operation.
	elems []types.Type
}

// isElem reports whether t is one of the tree's element types.
func (c treeCheck) isElem(t types.Type) bool {
	for _, e := range c.elems {
		if types.Identical(t, e) {
			return true
		}
	}
	return false
}

// isTree reports whether t is a slice of one of the tree's element types,
// whatever it is named.
func (c treeCheck) isTree(t types.Type) bool {
	s, isSlice := t.Underlying().(*types.Slice)
	return isSlice && c.isElem(s.Elem())
}

func (c treeCheck) node(n ast.Node) {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if c.isTree(c.pass.TypesInfo.TypeOf(n)) {
			c.pass.Reportf(n.Pos(), treeBuiltMessage)
		}
	case *ast.CallExpr:
		c.call(n)
	case *ast.Ident:
		c.instance(n)
	case *ast.AssignStmt:
		for _, lhs := range n.Lhs {
			c.target(lhs)
		}
	case *ast.IncDecStmt:
		c.target(n.X)
	case *ast.RangeStmt:
		c.target(n.Key)
		c.target(n.Value)
	case *ast.UnaryExpr:
		if pos, through := c.element(n.X); n.Op == token.AND && through {
			c.pass.Reportf(pos, treeAddressMessage)
		}
	default:
	}
}

// target reports e when writing it writes an element of the tree.
func (c treeCheck) target(e ast.Expr) {
	if pos, through := c.element(e); through {
		c.pass.Reportf(pos, treeWrittenMessage)
	}
}

// element returns where e reaches through an element of a tree slice, the
// outermost such index, and whether it does: an index of one, or a field,
// index or dereference of what one reaches.
func (c treeCheck) element(e ast.Expr) (token.Pos, bool) {
	for e != nil {
		switch x := ast.Unparen(e).(type) {
		case *ast.IndexExpr:
			if c.isTree(c.pass.TypesInfo.TypeOf(x.X)) {
				return x.Pos(), true
			}
			e = x.X
		case *ast.SelectorExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		default:
			return token.NoPos, false
		}
	}
	return token.NoPos, false
}

// call reports a builtin building or writing a tree slice, and sort.Slice or
// sort.SliceStable reordering one.
func (c treeCheck) call(call *ast.CallExpr) {
	if len(call.Args) == 0 || !c.isTree(c.pass.TypesInfo.TypeOf(call.Args[0])) {
		return
	}
	switch fn := typeutil.Callee(c.pass.TypesInfo, call).(type) {
	case *types.Builtin:
		switch fn.Name() {
		case "make":
			c.pass.Reportf(call.Pos(), treeBuiltMessage)
		case "append", "copy", "clear":
			c.pass.Reportf(call.Pos(), treeWrittenMessage)
		default:
		}
	case *types.Func:
		if fn.Pkg() != nil && fn.Pkg().Path() == "sort" && (fn.Name() == "Slice" || fn.Name() == "SliceStable") {
			c.pass.Reportf(call.Pos(), treeWrittenMessage)
		}
	default:
	}
}

// instance reports an instantiation, called or not, that builds or writes a
// tree slice: a slices function doing either, and a generic the module
// declares outside ir instantiated with a tree slice or element.
func (c treeCheck) instance(id *ast.Ident) {
	inst, ok := c.pass.TypesInfo.Instances[id]
	if !ok {
		return
	}
	generic := c.pass.TypesInfo.Uses[id]
	path := generic.Pkg().Path() // a generic is always declared in a package
	sig, isFunc := inst.Type.(*types.Signature)
	switch {
	case path == "slices":
		if c.isTree(inst.TypeArgs.At(0)) || isFunc && c.anyTree(sig.Results()) {
			c.slicesFunc(id, generic.Name())
		}
	case c.l.within(path, "ir"): // trusted, as its functions are
	case c.l.inModule(path) && c.treeArgs(inst.TypeArgs):
		c.pass.Reportf(id.Pos(), treeGenericMessage)
	default:
	}
}

// slicesFunc reports the function of package slices named name, instantiated
// over a tree slice, when it builds or writes one. The others read theirs; and
// those needing ordered or comparable elements, as Sort and Compact do, take
// no tree slice, whose elements are neither.
func (c treeCheck) slicesFunc(id *ast.Ident, name string) {
	switch name {
	case "Clone", "Collect", "Concat", "Grow", "Repeat", "SortedFunc", "SortedStableFunc":
		c.pass.Reportf(id.Pos(), treeBuiltMessage)
	case "AppendSeq", "CompactFunc", "Delete", "DeleteFunc", "Insert", "Replace", "Reverse", "SortFunc",
		"SortStableFunc":
		c.pass.Reportf(id.Pos(), treeWrittenMessage)
	default:
	}
}

// anyTree reports whether one of vars is a tree slice.
func (c treeCheck) anyTree(vars *types.Tuple) bool {
	for v := range vars.Variables() {
		if c.isTree(v.Type()) {
			return true
		}
	}
	return false
}

// treeArgs reports whether one of args is a tree slice or element.
func (c treeCheck) treeArgs(args *types.TypeList) bool {
	for t := range args.Types() {
		if c.isTree(t) || c.isElem(t) {
			return true
		}
	}
	return false
}
