package graphql

import (
	"os"
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	lookcompile "github.com/dexpace/morphic/internal/lintrules/testdata/mod/lookalike/compile"
	lookir "github.com/dexpace/morphic/internal/lintrules/testdata/mod/lookalike/ir"
	"github.com/dexpace/morphic/ir"
	irx "github.com/dexpace/morphic/ir"
)

func localWords(s string) string { return s }

func twoWords(s string) (string, string) { return s, s }

// named embeds a Naming, whose fields it promotes.
type named struct{ ir.Naming }

// common holds one as a field, as an ir.TypeCommon does.
type common struct{ Name ir.Naming }

// mirror has ir.Naming's fields without its tags, so it converts to one.
type mirror struct {
	Source    string
	Canonical string
	Hint      string
	Aliases   []string
}

// short lacks Aliases, so it is no look-alike of ir.Naming.
type short struct{ Source, Canonical, Hint string }

// others names its last field otherwise, so it is no look-alike either.
type others struct {
	Source, Canonical, Hint string
	Others                  []string
}

// pointer points to a Naming under a name of its own.
type pointer *ir.Naming

// embedsPointer promotes a Naming's fields through a pointer.
type embedsPointer struct{ *ir.Naming }

// genericMirror is ir.Naming's shape once T is a string.
type genericMirror[T ~string] struct {
	Source    T
	Canonical T
	Hint      T
	Aliases   []T
}

// fill writes the channels an instantiation makes ir.Naming's.
func (g *genericMirror[T]) fill(s T) {
	g.Canonical = s // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	g.Hint = s      // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}

// hinted builds one in generic code.
func hinted[T ~string](s T) genericMirror[T] {
	return genericMirror[T]{Hint: s} // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}

// fillUnnamed writes the channels of an unnamed struct of that shape.
func fillUnnamed[T ~string](n *struct {
	Source, Canonical, Hint T
	Aliases                 []T
}, s T) {
	n.Canonical = s // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}

func plantMirrors(name string, p pointer, e embedsPointer) (ir.Naming, ir.Naming) {
	keyed := mirror{Source: name, Canonical: localWords(name)}                 // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	keyed.Hint = localWords(name)                                              // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	unkeyed := mirror{name, localWords(name), compile.SubHint(name, "x"), nil} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	p.Hint = name                                                              // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	e.Canonical = name                                                         // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	near := short{Canonical: name}
	near.Hint = name
	_ = others{Canonical: name}
	_, _ = near, &os.Args // a package's variable is no struct's field
	var g genericMirror[string]
	g.fill(name)
	_, _ = hinted(name), ir.Naming(unkeyed)
	return ir.Naming(keyed), ir.Naming(g)
}

func plantNaming(name string, words []string, n *named, c *common, big ir.BigVal) []ir.Naming {
	declared := ir.Naming{Source: name, Canonical: localWords(name)}                  // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	hinted := ir.Naming{Hint: localWords(name)}                                       // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	carried := ir.Naming{Canonical: name}                                             // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	wrapped := ir.Naming{Canonical: localWords(ir.CanonicalWords(name))}              // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	spelled := ir.Naming{Canonical: lookir.CanonicalWords(name)}                      // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	spelledHint := ir.Naming{Hint: lookcompile.SubHint(name, "x")}                    // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	converted := ir.Naming{Canonical: string(name)}                                   // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	method := ir.Naming{Hint: big.String()}                                           // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	field := ir.Naming{Canonical: compile.NamingFor(name).Canonical}                  // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	positional := irx.Naming{name, localWords(name), compile.SubHint(name, "x"), nil} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	var late ir.Naming
	late.Canonical = strings.ToLower(name)    // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	late.Hint = localWords(name)              // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	n.Hint = name                             // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	promoted := named{Hint: localWords(name)} // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	promotedAll := named{Source: name, Canonical: ir.CanonicalWords(name), Hint: compile.SubHint(name, "x")}
	c.Name.Canonical = name // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	late.Canonical += "_x"  // want "namechannels: ir.Naming.Canonical is extended in place; fill it whole, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	var other string
	other, late.Hint = twoWords(name) // want "namechannels: ir.Naming.Hint is filled from a call returning several values; fill it alone, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	for _, late.Hint = range words {  // want "namechannels: ir.Naming.Hint is filled by a range clause; fill it from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	}
	fill(&late.Canonical)                                        // want "namechannels: the address of ir.Naming.Canonical is taken, so what fills it is out of sight; fill it from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	byKey := map[string]ir.Naming{"k": {Hint: localWords(name)}} // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	pointers := []*ir.Naming{{Canonical: name}}                  // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"

	late.Canonical, other = ir.CanonicalWords(name), name
	other, late.Hint = ir.CanonicalWords(name), name // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	(late.Hint) = localWords(name)                   // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	late.Hint = compile.SubHint(name, "item")
	canonical := ir.Naming{Source: name, Canonical: ir.CanonicalWords(name)}
	sub := ir.Naming{Hint: compile.SubHint(name, "item")}
	parenthesized := ir.Naming{Canonical: (ir.CanonicalWords(name))}
	made := compile.NamingFor(name)
	kept := ir.Naming{Source: name, Aliases: []string{name}}
	_, _, _, _, _, _ = byKey, pointers, other, converted, method, parenthesized
	_, _ = spelled, spelledHint
	_, _ = promoted, promotedAll
	return []ir.Naming{declared, hinted, carried, wrapped, field, positional, late, canonical, sub, made, kept,
		{Canonical: localWords(name)}} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}

func fill(s *string) { *s = "" }
