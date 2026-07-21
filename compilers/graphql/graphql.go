package graphql

import (
	"context"
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// Compiler lowers GraphQL SDL documents — plain schemas and Apollo Federation v1
// and v2 subgraphs — into the IR.
type Compiler struct{}

// New returns the GraphQL compiler.
func New() *Compiler { return &Compiler{} }

// Formats reports the source dialect this compiler accepts. GraphQL SDL is
// version-less; federation is detected from the document itself, not selected by
// format.
func (*Compiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{{Name: "graphql", Version: "sdl"}}
}

// Compile implements compilers.Compiler. It accepts one or more SDL sources —
// GraphQL schemas are routinely split across files — and parses them as one
// merged document. Spec problems become diagnostics; the error return is
// reserved for parser panics and programmer errors.
func (c *Compiler) Compile(_ context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	if len(sources) == 0 {
		return nil, nil, fmt.Errorf("graphql: expected at least one source, got 0")
	}
	formatOpts, err := optionsFrom(opts) // nil FormatOptions → defaults; wrong type → error
	if err != nil {
		return nil, nil, err
	}
	astInputs, infos, index := sourcesFrom(toLoadInputs(sources))
	ld, diags, err := load(infos, astInputs, index)
	if err != nil || ld == nil {
		return nil, diags, err
	}
	l := newLowerer(ld, formatOpts)
	out := l.run()
	//nolint:gocritic // deliberate concat: load diagnostics precede lowering diagnostics
	out.Diagnostics = append(diags, l.diags...)
	return out, out.Diagnostics, nil
}

// toLoadInputs adapts the pre-read compiler sources to the load layer's input
// shape.
func toLoadInputs(sources []compilers.Source) []loadInput {
	in := make([]loadInput, 0, len(sources))
	for _, s := range sources {
		in = append(in, loadInput{path: s.Path, data: s.Data})
	}
	return in
}

// optionsFrom resolves the compiler-specific options: a nil FormatOptions gets
// defaults, a graphql.Options value is normalized, and any other type is a
// programmer error.
func optionsFrom(opts compilers.Options) (Options, error) {
	switch fo := opts.FormatOptions.(type) {
	case nil:
		return Options{}.withDefaults(), nil
	case Options:
		return fo.withDefaults(), nil
	default:
		return Options{}, fmt.Errorf("graphql: FormatOptions must be graphql.Options, got %T", opts.FormatOptions)
	}
}

// lowerer is the single mutable context of one Compile call: a local, never a
// package global (invariant 5). It threads the merged definitions, the interning
// table, accumulated diagnostics, and the recursion depth counter through every
// lowering position.
type lowerer struct {
	doc         *ast.SchemaDocument
	srcIndex    map[*ast.Source]int
	sources     []ir.SourceInfo
	defs        map[string]*mergedDef
	roots       rootNames
	fedVersion  string // "", "1", or "2"
	out         *ir.Document
	opts        Options
	diags       []ir.Diagnostic
	byPointer   map[string]ir.TypeID
	unknownRefs map[string]bool // dangling type names already diagnosed
	depth       int
}

// newLowerer allocates a lowerer over one loaded document, resolving the
// definition map, root operation types, and federation version up front.
func newLowerer(ld *loaded, opts Options) *lowerer {
	defs, diags := buildDefs(ld.doc, ld.srcIndex)
	return &lowerer{
		doc:         ld.doc,
		srcIndex:    ld.srcIndex,
		sources:     ld.sources,
		defs:        defs,
		roots:       resolveRootNames(ld.doc, defs),
		fedVersion:  detectFederationVersion(ld.doc, defs),
		out:         &ir.Document{Types: ir.TypeRegistry{}},
		opts:        opts,
		diags:       diags,
		byPointer:   make(map[string]ir.TypeID),
		unknownRefs: make(map[string]bool),
	}
}

// run drives the lowering pipeline: named types first so operation return and
// argument types resolve to interned IDs, then the operation surface, then
// document metadata. It assembles and returns the Document.
func (l *lowerer) run() *ir.Document {
	l.lowerTypes()
	l.out.Services = []ir.Service{l.lowerService()}
	l.lowerMeta()
	l.out.IRVersion = ir.IRVersion
	l.out.Sources = l.sources
	return l.out
}

// intern returns the TypeID for pointer, building the node on first visit.
// Registering the ID before building is what terminates recursive types: a
// self-reference reached during build hits byPointer and returns the ID without
// re-entering build.
func (l *lowerer) intern(pointer string, id ir.TypeID, build func() ir.TypeDef) ir.TypeID {
	if existing, ok := l.byPointer[pointer]; ok {
		return existing
	}
	l.byPointer[pointer] = id
	l.out.Types[id] = build()
	return id
}

// primRef interns the primitive of kind k under its shared ID on first use and
// returns a reference to it. Primitives are leaves shared across formats, so they
// never enter the pointer-keyed interning table.
func (l *lowerer) primRef(k ir.PrimKind) ir.TypeRef {
	id := primTypeID(k)
	if _, ok := l.out.Types[id]; !ok {
		l.out.Types[id] = &ir.Primitive{TypeCommon: ir.TypeCommon{ID: id}, Prim: k}
	}
	return ir.TypeRef{Target: id}
}
