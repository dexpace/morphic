// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapitest

// PathKeyFragments maps Paths Object keys to where the fragment each carries
// begins, the index of its "#", or -1 where it carries none. The lowering and
// the rule table each split a key there (docs/compilers/openapi.md#path-keys),
// so both are held to this one table: a key split apart would lower to a
// template whose expressions its path parameters are not checked against.
func PathKeyFragments() map[string]int {
	return map[string]int{
		"":           -1,
		"/a":         -1,
		"/a#f":       2,
		"/a#":        2,
		"#a":         0,
		"/a#x#y":     2,
		"/a/{b#c}":   -1,
		"/a/{b#c}#d": 8,
		"/a{#}":      -1,
		"/a/{}#x":    5,
		"/a/{x#{y}":  5,
		"/{a{b}#c}":  6,
		"/a?x":       -1,
		"/a?q#x":     4,
		"/a?{q#x}":   5,
		"/%23#x":     4,
		"/é#x":       3,
	}
}
