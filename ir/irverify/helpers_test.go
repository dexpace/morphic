// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

// Package irverify_test holds the tests of the irverify package. The helpers
// several files share live here.
package irverify_test

import "github.com/dexpace/morphic/ir/irverify"

// violationPaths returns the paths of the violations of one code, in report
// order.
func violationPaths(vs []irverify.Violation, code string) []string {
	var out []string
	for _, violation := range vs {
		if violation.Code == code {
			out = append(out, violation.Path)
		}
	}
	return out
}

// violationCodes returns the codes of vs, for set-membership assertions that do
// not depend on violation order or message wording.
func violationCodes(vs []irverify.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, violation := range vs {
		out = append(out, violation.Code)
	}
	return out
}
