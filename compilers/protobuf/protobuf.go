package protobuf

import (
	"context"
	"fmt"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// Compiler lowers Protocol Buffers .proto schemas into the IR.
type Compiler struct{}

// New returns the protobuf compiler.
func New() *Compiler { return &Compiler{} }

// Formats reports the protobuf dialects this compiler accepts. proto2, proto3,
// and the 2023 edition share one lowering; the parser resolves edition features
// into the same descriptor surface the proto2/proto3 paths produce.
func (*Compiler) Formats() []compilers.SourceFormat {
	return []compilers.SourceFormat{
		{Name: "protobuf", Version: "2"},
		{Name: "protobuf", Version: "3"},
		{Name: "protobuf", Version: "2023"},
	}
}

// Compile implements compilers.Compiler. It accepts exactly one root .proto
// source; imports other than bundled well-known types are unresolved (the
// compiler does no file I/O) and reported as diagnostics.
func (c *Compiler) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	if len(sources) != 1 {
		return nil, nil, fmt.Errorf("protobuf: expected exactly one source, got %d", len(sources))
	}
	formatOpts, err := optionsFrom(opts) // nil FormatOptions → defaults; wrong type → error
	if err != nil {
		return nil, nil, err
	}
	loadedFile, diags, err := load(ctx, 0, sources[0], formatOpts)
	if err != nil || loadedFile == nil {
		return nil, diags, err
	}
	l := newLowerer(0, loadedFile, formatOpts)
	out := l.run() // types → extensions → service/operations → meta; assembles Document
	//nolint:gocritic // deliberate concat: load diagnostics precede lowering diagnostics
	out.Diagnostics = append(diags, l.diags...)
	return out, out.Diagnostics, nil
}

// run drives the lowering pipeline over one linked file. Order matters: all
// declared messages and enums are hoisted first so field, extension, and rpc
// references find interned IDs; then extension fields are attached to the
// messages they extend; then the service walk; then file-level metadata. It
// assembles and returns the Document.
func (l *lowerer) run() *ir.Document {
	l.lowerTypes()
	l.lowerExtensions()
	l.out.Services = []ir.Service{l.lowerService()}
	l.lowerMeta()
	l.out.IRVersion = ir.IRVersion
	l.out.Sources = []ir.SourceInfo{l.source}
	return l.out
}

// optionsFrom resolves the compiler-specific options: a nil FormatOptions gets
// defaults, a protobuf.Options value is normalized, and any other type is a
// programmer error.
func optionsFrom(opts compilers.Options) (Options, error) {
	switch fo := opts.FormatOptions.(type) {
	case nil:
		return Options{}.withDefaults(), nil
	case Options:
		return fo.withDefaults(), nil
	default:
		return Options{}, fmt.Errorf("protobuf: FormatOptions must be protobuf.Options, got %T", opts.FormatOptions)
	}
}
