package report_test

import (
	"errors"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// errDisk is the failure the refusing writers return.
var errDisk = errors.New("disk gone")

// failingWriter accepts writes until the failAt-th, which fails, and counts
// every attempt it is asked for, including any after the failure, so a renderer
// that does not stop shows in attempts.
type failingWriter struct {
	failAt   int
	attempts int
	text     strings.Builder
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.attempts++
	if w.attempts == w.failAt {
		return 0, errDisk
	}
	return w.text.Write(p)
}

// diag returns an error diagnostic with message "m" at prov.
func diag(code string, prov ir.Provenance) ir.Diagnostic {
	return ir.Diagnostic{Severity: ir.SeverityError, Code: code, Message: "m", Provenance: prov}
}
