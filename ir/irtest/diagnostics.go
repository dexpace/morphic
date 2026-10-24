package irtest

import "github.com/dexpace/morphic/ir"

// DiagCodes lists the code of each of diags, in order.
func DiagCodes(diags []ir.Diagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}
