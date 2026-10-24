// Package irverify_test holds the tests of the irverify package. The helpers
// several files share live here.
package irverify_test

import "github.com/dexpace/morphic/ir/irverify"

// violationPaths returns the paths of the violations of one code, in report
// order.
func violationPaths(vs []irverify.Violation, code string) []string {
	var out []string
	for _, v := range vs {
		if v.Code == code {
			out = append(out, v.Path)
		}
	}
	return out
}

// violationCodes returns the codes of vs, for set-membership assertions that do
// not depend on violation order or message wording.
func violationCodes(vs []irverify.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Code)
	}
	return out
}
