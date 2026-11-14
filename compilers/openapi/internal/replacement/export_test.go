// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package replacement

// CodeSpans is the content of each span codeSpans finds, for the external
// tests.
func CodeSpans(text string) []string {
	spans := codeSpans(text)
	if len(spans) == 0 {
		return nil
	}
	out := make([]string, 0, len(spans))
	for _, n := range spans {
		out = append(out, n.text)
	}
	return out
}
