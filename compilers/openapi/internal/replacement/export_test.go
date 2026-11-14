// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

// CodeSpans is the content of each span codeSpans finds, for the external
// tests.
func CodeSpans(text string) []string {
	var out []string
	for _, n := range codeSpans(text) {
		out = append(out, n.text)
	}
	return out
}
