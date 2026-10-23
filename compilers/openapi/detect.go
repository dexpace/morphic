package openapi

import (
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/detect"
	"github.com/dexpace/morphic/ir"
)

// Detect implements compilers.Compiler. It reports the dialect src declares by
// its version's major.minor prefix, naming swagger@2.0 too, which it does not
// serve, so the caller can say unsupported rather than unreadable. The path is
// not consulted.
//
// It parses all of src, whatever its format, and hands Compile the parse in
// Recognition.Parsed. The byte budget bounds that cost: a source past it is
// declined unread with Compile's own refusal.
//
// Other declines say why only when the source is this compiler's own: it
// declares a version under a discriminating key and will not parse, or its
// root merges deeper than detection reads.
func (*Compiler) Detect(src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	// Source 0 is src: the one source detection is handed is the whole of the
	// table a declined diagnostic can index (compilers.Compiler.Detect).
	return detect.Recognize(rootSrcIndex, src.Data, detectionBudget(opts))
}

// detectionBudget is the byte budget detection is held to: the one opts would
// compile with. Options of another compiler's type are not this one's to read,
// so they mean the defaults here — Compile is what reports them, and only if
// the source turns out to be this compiler's.
func detectionBudget(opts compilers.Options) int {
	o, err := optionsFrom(opts)
	if err != nil {
		o = Options{}.withDefaults()
	}
	return bounded(o.Limits.MaxSourceBytes)
}
