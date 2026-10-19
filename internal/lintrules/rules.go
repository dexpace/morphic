//go:build ruleguard

// Package lintrules holds the ruleguard rules golangci-lint runs through
// gocritic. The build tag keeps it out of every build: ruleguard reads the file
// as rules, and nothing imports it. scripts/lint-planted.sh holds each rule to
// the findings planted for it under testdata, since a clean tree cannot tell a
// rule from one that recognizes nothing. Run it after editing this file: a warm
// golangci-lint cache does not key on it, and replays findings from before.
package lintrules

import "github.com/quasilyte/go-ruleguard/dsl"

// registryWrites refuses building or writing an ir.TypeRegistry outside ir and
// compilers/compile. Reaching the registry only through compile.Types is what
// makes one node per source coordinate (invariant 3) mechanical rather than a
// convention each compiler re-honours. The rules match types, not spellings, so
// an aliased import or a registry held in a local is still one. A test file may
// build one, as fixtures do. A compile.Settlement's rewrite of the registry it
// is handed is the framework's own; .golangci.yml excuses each by its line.
func registryWrites(m dsl.Matcher) {
	m.Import("github.com/dexpace/morphic/ir")

	// The DSL takes no local variables, so each Where spells the condition out.
	m.Match(`$t{$*_}`, `make($t)`, `make($t, $_)`, `$t($_)`).
		Where(m["t"].Type.Is(`ir.TypeRegistry`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(ir|compilers/compile)(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types`)

	m.Match(`maps.Clone($r)`).
		Where(m["r"].Type.Is(`ir.TypeRegistry`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(ir|compilers/compile)(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`builds an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types`)

	// gogrep binds a variadic wildcard once, to its first fit, so one pattern
	// cannot find a registry at any position of a multiple assignment: it would
	// settle on the first index expression and never try the next. Each position
	// gets its own, to the third.
	m.Match(`$r[$_], $*_ = $*_`, `$_, $r[$_], $*_ = $*_`, `$_, $_, $r[$_], $*_ = $*_`,
		`delete($r, $_)`, `clear($r)`,
		`maps.Copy($r, $_)`, `maps.Insert($r, $_)`, `maps.DeleteFunc($r, $_)`).
		Where(m["r"].Type.Is(`ir.TypeRegistry`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(ir|compilers/compile)(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types`)
}
