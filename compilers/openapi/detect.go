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
// It parses all of src, handing Compile the parse in Recognition.Parsed. Two
// sources are declined with an error whatever their format: one past the byte
// budget, unread, with Compile's own refusal, and one whose root merges deeper
// than detection reads, its own keys leaving the version open. Any other
// decline is silent unless src declares a version under a discriminating key
// and is unreadable.
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
