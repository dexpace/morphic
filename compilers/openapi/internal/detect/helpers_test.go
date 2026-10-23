package detect

import (
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// recognize is Recognize as Detect calls it on the one source it is handed,
// source 0, with no byte budget: no case here comes near the default, and
// TestRecognize_HoldsTheParseToItsByteBudget sets one of its own.
func recognize(src string) (compilers.Recognition, []ir.Diagnostic, bool) {
	return Recognize(0, []byte(src), 0)
}

// codesOf reduces diagnostics to their codes, which is the whole of what the
// detection tests assert about them: the message carries a parser's wording and
// pinning it would test yaml.v3 rather than this package.
func codesOf(diags []ir.Diagnostic) []string {
	if len(diags) == 0 {
		return nil
	}
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return codes
}

// scanCutover is the size past which detection used to scan bytes rather than
// parse, and still the size where the change of reading shows: a case larger
// than this pins that a large document is read as a small one is.
const scanCutover = 64 << 10

// flowPad returns a run of bytes long enough that a flow entry holding it puts
// everything after it past scanCutover.
func flowPad() string { return strings.Repeat("p", scanCutover) }

// padTo returns src grown past scanCutover by appending filler.
func padTo(src, filler string) string {
	var b strings.Builder
	b.WriteString(src)
	for b.Len() <= scanCutover {
		b.WriteString(filler)
	}
	return b.String()
}

// dupRepeats is how many times the fixtures below repeat a root key. It is not a
// threshold: the wrong answer reproduces at two repeats, and this many only
// makes the fixture recognizably a document rather than a corner. It is
// deliberately far below the 6,553 of the report — yaml.v3 raises one error per
// pair of matching keys, so that count produced 21,467,628 of them and a 1.2 GB
// message, and a fixture that large turns a revert into an out-of-memory kill
// instead of a failing assertion. What guards the cost is
// TestSniff_CostIsNotQuadraticInRepeatedKeys, which measures growth rather than
// paying for it.
const dupRepeats = 512
