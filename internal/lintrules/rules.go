//go:build ruleguard

// Package lintrules holds the ruleguard rules golangci-lint runs through
// gocritic. The build tag keeps it out of every build: ruleguard reads the file
// as rules, and nothing imports it. scripts/lint-planted.sh holds each rule to
// the findings planted for it under testdata, since a clean tree cannot tell a
// rule from one that recognizes nothing. Run it after editing this file: a warm
// golangci-lint cache does not key on it, and replays findings from before.
package lintrules

import (
	"github.com/quasilyte/go-ruleguard/dsl"
	"github.com/quasilyte/go-ruleguard/dsl/types"
)

// registryWrites refuses building or writing an ir.TypeRegistry outside ir and
// compilers/compile. Reaching the registry only through compile.Types is what
// makes one node per source coordinate (invariant 3) mechanical rather than a
// convention each compiler re-honours. The rules match types, not spellings, so
// an aliased import or a registry held in a local is still one. A test file may
// build one, as fixtures do. A compile.Settlement's rewrite of the registry it
// is handed is the framework's own; .golangci.yml excuses each by its line.
func registryWrites(m dsl.Matcher) {
	m.Import("github.com/dexpace/morphic/ir")

	// A rule function takes no local variables, so each Where spells the
	// condition out.
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
	//
	// Two writes stay out of reach of a rule that reads types at the site: one
	// inside a generic helper, which sees only its type parameter (the maps
	// functions are named for that reason), and an unnamed
	// map[ir.TypeID]ir.TypeDef assigned where a registry is expected.
	m.Match(`$r[$_], $*_ = $*_`, `$_, $r[$_], $*_ = $*_`, `$_, $_, $r[$_], $*_ = $*_`,
		`for $r[$_] = range $_ { $*_ }`, `for $r[$_], $_ = range $_ { $*_ }`,
		`for $_, $r[$_] = range $_ { $*_ }`,
		`delete($r, $_)`, `clear($r)`,
		`maps.Copy($r, $_)`, `maps.Insert($r, $_)`, `maps.DeleteFunc($r, $_)`).
		Where(m["r"].Type.Is(`ir.TypeRegistry`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(ir|compilers/compile)(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`writes an ir.TypeRegistry outside ir and compilers/compile; go through compile.Types`)
}

// fragmentReadings refuses, outside compilers/openapi/internal/fragment, the two
// readings of a $ref fragment fragment.Pointer replaces (GitHub #911):
// url.QueryUnescape, and the library's references.Reference.GetJSONPointer,
// which is built on it and reads '+' as a space where RFC 3986 reads a plus. A
// reader of its own parts company with every other on the first '+'. The
// method is matched through a value, a pointer or an embedding; a look-alike
// method, even one of the same signature, is not it. Test files are not held.
func fragmentReadings(m dsl.Matcher) {
	m.Import("net/url")
	m.Import("github.com/speakeasy-api/openapi/references")

	m.Match(`url.QueryUnescape`).
		Where(!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/compilers/openapi/internal/fragment$`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`url.QueryUnescape reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer`)

	m.Match(`$x.GetJSONPointer`).
		Where((m["x"].Type.Is(`references.Reference`) || m["x"].Type.Is(`*references.Reference`) ||
			m["x"].Filter(embedsReference)) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/compilers/openapi/internal/fragment$`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`references.Reference.GetJSONPointer reads a $ref fragment otherwise than fragment.Pointer; use fragment.Pointer`)
}

// deref returns what t points to, or t when it is no pointer.
func deref(t types.Type) types.Type {
	if types.AsPointer(t) != nil {
		return types.AsPointer(t).Elem()
	}
	return t
}

// embedsDirectly reports whether t, a struct, embeds ref or a pointer to it.
func embedsDirectly(t, ref types.Type) bool {
	if types.AsStruct(t.Underlying()) == nil {
		return false
	}
	j := 0
	for j < types.AsStruct(t.Underlying()).NumFields() {
		if types.AsStruct(t.Underlying()).Field(j).Embedded() &&
			types.Identical(deref(types.AsStruct(t.Underlying()).Field(j).Type()), ref) {
			return true
		}
		j++
	}
	return false
}

// embedsReference reports whether ctx.Type, through a pointer, embeds
// references.Reference, itself or behind one more embedded struct, so the
// promoted GetJSONPointer is the library's. The interpreter running it has no
// C-style loop and holds no pointer type in a local, hence the shape.
func embedsReference(ctx *dsl.VarFilterContext) bool {
	ref := ctx.GetType(`github.com/speakeasy-api/openapi/references.Reference`)
	t := deref(ctx.Type)
	if types.AsStruct(t.Underlying()) == nil {
		return false
	}
	i := 0
	for i < types.AsStruct(t.Underlying()).NumFields() {
		if types.AsStruct(t.Underlying()).Field(i).Embedded() {
			ft := deref(types.AsStruct(t.Underlying()).Field(i).Type())
			if types.Identical(ft, ref) || embedsDirectly(ft, ref) {
				return true
			}
		}
		i++
	}
	return false
}

// nameChannels refuses filling ir.Naming's Canonical or Hint outside ir and
// compilers/compile with anything but a call into one of them. Those fields are
// ABI, so a compiler's own segmentation would make one mean two things (GitHub
// #163, #54). The call must be the value itself: a framework call wrapped in a
// local one, or a name carried in a variable, is refused. An element literal
// that elides ir.Naming is matched by its field names, which no other type
// carries. Test files are not held.
func nameChannels(m dsl.Matcher) {
	m.Import("github.com/dexpace/morphic/ir")

	// The framework call is told by how it is spelled, compile.X or ir.X, so an
	// import of either under another name reads as a local call. ruleguard's
	// Contains would read the import, but its answer changed with which other
	// packages a run loaded.

	m.Match(`ir.Naming{$*_, Canonical: $f($*_), $*_}`, `ir.Naming{$*_, Hint: $f($*_), $*_}`,
		`{$*_, Canonical: $f($*_), $*_}`, `{$*_, Hint: $f($*_), $*_}`,
		`$x.Canonical = $f($*_)`, `$x.Hint = $f($*_)`).
		Where(!m["f"].Text.Matches(`^(compile|ir)\.`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(ir|compilers/compile)(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords`)

	m.Match(`ir.Naming{$*_, Canonical: $v, $*_}`, `ir.Naming{$*_, Hint: $v, $*_}`,
		`{$*_, Canonical: $v, $*_}`, `{$*_, Hint: $v, $*_}`,
		`$x.Canonical = $v`, `$x.Hint = $v`).
		Where(!m["v"].Node.Is(`CallExpr`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(ir|compilers/compile)(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords`)
}

// idDerivations refuses, in a compiler other than compilers/compile, building
// an ir ID out of a string, and building a compile.Space out of anything but a
// string literal. The derivation stays with the compiler, but the grammar
// around it does not: the kind prefix, the namespace, and a minted node's
// namespace of its own (GitHub #162). pass and irverify convert an existing ID
// back to look a node up, which derives nothing, so only compilers are held,
// with the plants under internal/lintrules/testdata. Test files are not held.
func idDerivations(m dsl.Matcher) {
	m.Import("github.com/dexpace/morphic/ir")
	m.Import("github.com/dexpace/morphic/compilers/compile")

	m.Match(`$t($_)`).
		Where((m["t"].Type.Is(`ir.TypeID`) || m["t"].Type.Is(`ir.OpID`) || m["t"].Type.Is(`ir.PropID`) ||
			m["t"].Type.Is(`ir.AuthID`) || m["t"].Type.Is(`ir.ServiceID`) || m["t"].Type.Is(`ir.GroupID`) ||
			m["t"].Type.Is(`ir.ParamID`) || m["t"].Type.Is(`ir.EnumMemberID`) || m["t"].Type.Is(`ir.ChannelID`) ||
			m["t"].Type.Is(`ir.MessageID`)) &&
			m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(compilers|internal/lintrules/testdata)/`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/compilers/compile(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`an ir ID is converted from a string outside compilers/compile; derive it through the framework`)

	m.Match(`const $_ $t = $v`, `var $_ $t = $v`).
		Where((m["t"].Type.Is(`ir.TypeID`) || m["t"].Type.Is(`ir.OpID`) || m["t"].Type.Is(`ir.PropID`) ||
			m["t"].Type.Is(`ir.AuthID`) || m["t"].Type.Is(`ir.ServiceID`) || m["t"].Type.Is(`ir.GroupID`) ||
			m["t"].Type.Is(`ir.ParamID`) || m["t"].Type.Is(`ir.EnumMemberID`) || m["t"].Type.Is(`ir.ChannelID`) ||
			m["t"].Type.Is(`ir.MessageID`)) &&
			m["v"].Text.Matches("[\"`]") &&
			m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(compilers|internal/lintrules/testdata)/`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/compilers/compile(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`an ir ID is declared from a string literal outside compilers/compile; derive it through the framework`)

	m.Match(`compile.Space($x)`, `const $_ compile.Space = $x`, `var $_ compile.Space = $x`).
		Where((!m["x"].Node.Is(`BasicLit`) || !m["x"].Text.Matches("^[\"`]")) &&
			m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/(compilers|internal/lintrules/testdata)/`) &&
			!m.File().PkgPath.Matches(`^github\.com/dexpace/morphic/compilers/compile(/|$)`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report(`a compile.Space is built from a non-literal; a namespace is a string literal, or the path glued onto it loses its separator`)
}
