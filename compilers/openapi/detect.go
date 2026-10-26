package openapi

import (
	"context"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/detect"
	"github.com/dexpace/morphic/compilers/openapi/internal/load"
	"github.com/dexpace/morphic/ir"
)

// Detect implements compilers.Compiler. It reports the dialect src declares by
// its version's major.minor prefix, naming swagger@2.0 too, which it does not
// serve, so the caller can say unsupported rather than unreadable. The path is
// not consulted.
//
// It parses all of src, handing Compile the parse in Recognition.Parsed. Two
// sources are declined with an error whatever their format: one past
// SourceLimit, unread, with Compile's own refusal, and one whose root merges
// deeper than detection reads, its own keys leaving the version open. Any
// other decline is silent unless src declares a version under a discriminating
// key and is unreadable.
func (*Compiler) Detect(ctx context.Context, src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	// Source 0 is src: the one source detection is handed is the whole of the
	// table a declined diagnostic can index (compilers.Compiler.Detect).
	return detect.Recognize(ctx, rootSrcIndex, src.Data, sourceBytes(opts))
}

// SourceLimit implements compilers.Compiler: the byte budget,
// Limits.MaxSourceBytes, of the options opts would compile with, spelled as
// the contract spells one (load.ReadLimit).
func (*Compiler) SourceLimit(opts compilers.Options) int {
	return load.ReadLimit(sourceBytes(opts))
}

// sourceBytes is the byte budget of the options opts would compile with: the
// one the load phase is handed. Options of another compiler's type are not
// this one's to read, so they mean the defaults here — Compile is what reports
// them, and only if the source turns out to be this compiler's.
func sourceBytes(opts compilers.Options) budget.Limit {
	o, err := optionsFrom(opts)
	if err != nil {
		o = Options{}.withDefaults()
	}
	return loadOptions(o, nil, compilers.Source{}).MaxSourceBytes
}
