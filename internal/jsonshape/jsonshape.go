package jsonshape

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/internal/decls"
)

// Format numbers the way Describe spells a shape. It moves whenever the
// description of an unchanged shape changes, so a pin that records it beside
// each description can tell a shape that moved from one described anew.
const Format = 1

// maxTypes bounds the named types one description holds. A package declares
// finitely many, so reaching it means the walk went wrong, not that the
// schema is large.
const maxTypes = 4096

// maxDepth bounds how deeply one type expression nests: []map[K]*V nests three
// deep.
const maxDepth = 32

// Describe returns the JSON shape of the named type root, which p declares,
// and of every type it reaches: one entry per named type, sorted by name. It
// refuses a type whose shape it cannot read from the declarations, such as one
// held in a non-sealed interface, rather than describe less than the encoder
// reads.
func Describe(p *decls.Package, root string) (string, error) {
	if p == nil {
		return "", errors.New("jsonshape: no package")
	}
	start, err := p.Named(root)
	if err != nil {
		return "", fmt.Errorf("jsonshape: %w", err)
	}

	w := &walker{
		pkg:     p,
		methods: methodDecls(p),
		consts:  map[*types.Package]map[*types.Named][]*types.Const{},
		seen:    map[*types.Named]bool{},
	}
	w.reach(start)
	var entries []entry
	for len(w.queue) > 0 {
		if len(entries) == maxTypes {
			return "", fmt.Errorf("jsonshape: %s reaches more than %d types", root, maxTypes)
		}
		n := w.queue[0]
		w.queue = w.queue[1:]
		e, err := w.describe(n)
		if err != nil {
			return "", fmt.Errorf("jsonshape: %s: %w", w.name(n), err)
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.name, b.name) })

	// The header names the package, not its import path, which is no part of
	// the shape: moving the module must not move a pin.
	var b strings.Builder
	fmt.Fprintf(&b, "jsonshape %d: %s.%s\n", Format, p.Types.Name(), root)
	for _, e := range entries {
		fmt.Fprintf(&b, "\ntype %s %s\n", e.name, e.head)
		for _, line := range e.lines {
			fmt.Fprintf(&b, "\t%s\n", line)
		}
	}
	return b.String(), nil
}

// entry is one named type's part of a description.
type entry struct {
	// name spells the type: bare when the described package declares it,
	// qualified by import path otherwise.
	name string
	// head is what the type is defined as: struct, interface, or a type
	// expression such as string or map[K]V.
	head string
	// lines are the members, variants, methods and constants, each group
	// sorted, so a reordered declaration describes the same shape.
	lines []string
}

// walker describes the named types one root reaches, each once, in the order
// it reaches them: queue holds those reached and not yet described.
type walker struct {
	pkg *decls.Package
	// methods finds the declaration of a method the package declares, for a
	// sum's Kind, which is read from its body.
	methods map[*types.Func]*ast.FuncDecl
	// consts indexes each package's constants by type, built the first time a
	// type of that package is described.
	consts map[*types.Package]map[*types.Named][]*types.Const
	seen   map[*types.Named]bool
	queue  []*types.Named
}

// methodDecls indexes the methods p declares by the object each defines.
func methodDecls(p *decls.Package) map[*types.Func]*ast.FuncDecl {
	out := map[*types.Func]*ast.FuncDecl{}
	for _, file := range p.Syntax {
		for _, d := range file.Decls {
			fd, isFunc := d.(*ast.FuncDecl)
			if !isFunc || fd.Recv == nil {
				continue
			}
			if fn, isMethod := p.Info.Defs[fd.Name].(*types.Func); isMethod {
				out[fn] = fd
			}
		}
	}
	return out
}

// reach queues n to be described, unless it already is.
func (w *walker) reach(n *types.Named) {
	if w.seen[n] {
		return
	}
	w.seen[n] = true
	w.queue = append(w.queue, n)
}

// name spells the named type n as a description does.
func (w *walker) name(n *types.Named) string {
	return types.TypeString(n, w.qualifier)
}

// qualifier spells a package in a type string: not at all for the package
// described, and by its import path for any other.
func (w *walker) qualifier(pkg *types.Package) string {
	if pkg == w.pkg.Types {
		return ""
	}
	return pkg.Path()
}

// describe returns n's entry, queueing every named type it reaches. An
// instance has its origin's type parameters, so one check refuses both.
func (w *walker) describe(n *types.Named) (entry, error) {
	if n.TypeParams().Len() > 0 {
		return entry{}, errors.New("a generic type has a shape per instantiation, not one")
	}
	if n.Obj().Pkg() == nil {
		return entry{}, errors.New("a predeclared type has no declarations to read")
	}
	e := entry{name: w.name(n)}
	var err error
	switch u := n.Underlying().(type) {
	case *types.Struct:
		e.head = "struct"
		e.lines, err = w.members(u)
	case *types.Interface:
		e.head = "interface"
		e.lines, err = w.variants(n, u)
	default:
		e.head, err = w.spell(u, 0)
	}
	if err != nil {
		return entry{}, err
	}
	e.lines = append(e.lines, w.codec(n)...)
	e.lines = append(e.lines, w.constants(n)...)
	return e, nil
}

// spell writes t as a description does, queueing each named type in it. It
// refuses a type the JSON encoder cannot encode, or encodes by what a value
// holds rather than by its declaration, such as an unnamed interface.
func (w *walker) spell(t types.Type, depth int) (string, error) {
	if depth > maxDepth {
		return "", fmt.Errorf("a type nests more than %d deep", maxDepth)
	}
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		w.reach(t)
		return w.name(t), nil
	case *types.Basic:
		return basic(t)
	case *types.Pointer:
		elem, err := w.spell(t.Elem(), depth+1)
		return "*" + elem, err
	case *types.Slice:
		elem, err := w.spell(t.Elem(), depth+1)
		return "[]" + elem, err
	case *types.Array:
		elem, err := w.spell(t.Elem(), depth+1)
		return fmt.Sprintf("[%d]%s", t.Len(), elem), err
	case *types.Map:
		key, err := w.spell(t.Key(), depth+1)
		if err != nil {
			return "", err
		}
		elem, err := w.spell(t.Elem(), depth+1)
		return "map[" + key + "]" + elem, err
	default:
		return "", fmt.Errorf("%s has no shape a description spells", t)
	}
}

// basic spells a basic type by its kind, so byte and uint8 are one type, and
// refuses one JSON has no encoding for. No declared type is untyped, so none
// reaches here.
func basic(b *types.Basic) (string, error) {
	if b.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsString) == 0 {
		return "", fmt.Errorf("%s has no JSON encoding", b)
	}
	return types.Typ[b.Kind()].Name(), nil
}

// members lists a struct's embeddings, then its members by JSON name. A field
// the encoder ignores is left out, so adding one changes no shape.
func (w *walker) members(s *types.Struct) ([]string, error) {
	var embeds, fields []string
	for i := range s.NumFields() {
		line, embedded, err := w.member(s.Field(i), s.Tag(i))
		switch {
		case err != nil:
			return nil, fmt.Errorf("field %s: %w", s.Field(i).Name(), err)
		case line == "":
		case embedded:
			embeds = append(embeds, line)
		default:
			fields = append(fields, line)
		}
	}
	slices.Sort(embeds)
	slices.Sort(fields)
	return append(embeds, fields...), nil
}

// member spells one field: its JSON name, type and tag options, or the type it
// embeds. It returns no line for a field the encoder ignores: one tagged "-",
// or an unexported one it does not embed.
func (w *walker) member(f *types.Var, rawTag string) (line string, embedded bool, err error) {
	tag := reflect.StructTag(rawTag).Get("json")
	if tag == "-" || (!f.Exported() && !f.Embedded()) {
		return "", false, nil
	}
	name, opts, err := parseTag(tag)
	if err != nil {
		return "", false, err
	}
	typ, err := w.spell(f.Type(), 0)
	if err != nil {
		return "", false, err
	}

	if i := slices.Index(opts, "embed"); i >= 0 || (f.Embedded() && name == "") {
		if i >= 0 {
			opts = slices.Delete(opts, i, i+1)
		}
		return strings.Join(append([]string{"embed", typ}, opts...), " "), true, nil
	}
	if name == "" {
		name = f.Name()
	}
	return strings.Join(append([]string{strconv.Quote(name), typ}, opts...), " "), false, nil
}

// parseTag splits a json tag into its name and its options, sorted, since
// their order means nothing. It refuses the spellings it would have to read
// the encoder's grammar for: a quoted or escaped name, and an empty option.
func parseTag(tag string) (name string, opts []string, err error) {
	if strings.ContainsAny(tag, "'\"`\\") {
		return "", nil, fmt.Errorf("json tag %q quotes or escapes, which a description does not read", tag)
	}
	name, rest, hasOpts := strings.Cut(tag, ",")
	if !hasOpts {
		return name, nil, nil
	}
	opts = strings.Split(rest, ",")
	if slices.Contains(opts, "") {
		return "", nil, fmt.Errorf("json tag %q has an empty option", tag)
	}
	slices.Sort(opts)
	return name, opts, nil
}

// codec lists the methods of n the encoder calls in place of reading its
// fields or kind, and the IsZero that omitzero asks, each with the receiver it
// is declared for: a method only *T has is not called on a T held by value.
func (w *walker) codec(n *types.Named) []string {
	byValue := types.NewMethodSet(n)
	var out []string
	for sel := range types.NewMethodSet(types.NewPointer(n)).Methods() {
		fn, isFunc := sel.Obj().(*types.Func)
		if !isFunc || !isCodec(fn.Name()) {
			continue
		}
		recv := "*" + w.name(n)
		if byValue.Lookup(fn.Pkg(), fn.Name()) != nil {
			recv = w.name(n)
		}
		out = append(out, fmt.Sprintf("method (%s) %s%s", recv, fn.Name(), w.signature(fn)))
	}
	slices.Sort(out)
	return out
}

// isCodec reports whether encoding/json/v2 calls a method of this name on a
// value it encodes or decodes: its own marshaler interfaces, v1's, the text
// forms, and IsZero for omitzero.
func isCodec(name string) bool {
	switch name {
	case "MarshalJSONTo", "UnmarshalJSONFrom", "MarshalJSON", "UnmarshalJSON",
		"AppendText", "MarshalText", "UnmarshalText", "IsZero":
		return true
	default:
		return false
	}
}

// signature spells fn's parameters and results without their names, which
// mean nothing to the encoder.
func (w *walker) signature(fn *types.Func) string {
	sig := fn.Signature()
	unnamed := func(t *types.Tuple) *types.Tuple {
		vars := make([]*types.Var, 0, t.Len())
		for v := range t.Variables() {
			vars = append(vars, types.NewParam(token.NoPos, nil, "", v.Type()))
		}
		return types.NewTuple(vars...)
	}
	bare := types.NewSignatureType(nil, nil, nil, unnamed(sig.Params()), unnamed(sig.Results()), sig.Variadic())
	return strings.TrimPrefix(types.TypeString(bare, w.qualifier), "func")
}

// constants lists the constants n's package declares of type n, in value
// order. A string or a boolean says in the JSON what it means, so it is listed
// by value alone, once however many names it has. A number says nothing of
// what it means; its constant's name does, so a numeric constant is listed
// with its name, and reordering iota flags changes the description.
func (w *walker) constants(n *types.Named) []string {
	pkg := n.Obj().Pkg()
	byType, indexed := w.consts[pkg]
	if !indexed {
		byType = constsByType(pkg)
		w.consts[pkg] = byType
	}
	consts := slices.Clone(byType[n])
	slices.SortFunc(consts, func(a, b *types.Const) int {
		return cmp.Or(compareConstants(a.Val(), b.Val()), strings.Compare(a.Name(), b.Name()))
	})
	out := make([]string, len(consts))
	for i, c := range consts {
		out[i] = constLine(c)
	}
	return slices.Compact(out)
}

// constLine spells one constant: a number with the name that says what it
// means, anything else by value alone.
func constLine(c *types.Const) string {
	v := c.Val()
	if v.Kind() == constant.Int || v.Kind() == constant.Float {
		return fmt.Sprintf("const %s = %s", c.Name(), v.ExactString())
	}
	return "const " + v.ExactString()
}

// compareConstants orders two constants of one type by value: numbers and
// strings as Go orders them, and booleans false before true, since a boolean
// has no less-than to ask.
func compareConstants(a, b constant.Value) int {
	if a.Kind() == constant.Bool {
		return strings.Compare(a.ExactString(), b.ExactString())
	}
	switch {
	case constant.Compare(a, token.LSS, b):
		return -1
	case constant.Compare(a, token.GTR, b):
		return 1
	default:
		return 0
	}
}

// constsByType indexes the package-level constants pkg declares by their named
// type, read through an alias, so one scan serves every type of the package.
func constsByType(pkg *types.Package) map[*types.Named][]*types.Const {
	out := map[*types.Named][]*types.Const{}
	for _, name := range pkg.Scope().Names() {
		c, isConst := pkg.Scope().Lookup(name).(*types.Const)
		if !isConst {
			continue
		}
		if named, isNamed := types.Unalias(c.Type()).(*types.Named); isNamed {
			out[named] = append(out[named], c)
		}
	}
	return out
}
