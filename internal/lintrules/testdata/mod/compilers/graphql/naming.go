package graphql

import (
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
	irx "github.com/dexpace/morphic/ir"
)

func localWords(s string) string { return s }

func twoWords(s string) (string, string) { return s, s }

// named embeds a Naming, whose fields it promotes.
type named struct{ ir.Naming }

// common holds one as a field, as an ir.TypeCommon does.
type common struct{ Name ir.Naming }

func plantNaming(name string, words []string, n *named, c *common, big ir.BigVal) []ir.Naming {
	declared := ir.Naming{Source: name, Canonical: localWords(name)}                  // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	hinted := ir.Naming{Hint: localWords(name)}                                       // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	carried := ir.Naming{Canonical: name}                                             // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	wrapped := ir.Naming{Canonical: localWords(ir.CanonicalWords(name))}              // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	converted := ir.Naming{Canonical: string(name)}                                   // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	method := ir.Naming{Hint: big.String()}                                           // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	field := ir.Naming{Canonical: compile.NamingFor(name).Canonical}                  // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	positional := irx.Naming{name, localWords(name), compile.SubHint(name, "x"), nil} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	var late ir.Naming
	late.Canonical = strings.ToLower(name) // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	late.Hint = localWords(name)           // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	n.Hint = name                          // want "namechannels: ir.Naming.Hint is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	c.Name.Canonical = name                // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
	late.Canonical += "_x"                 // want "namechannels: ir.Naming.Canonical is extended in place; fill it whole, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
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
	return []ir.Naming{declared, hinted, carried, wrapped, field, positional, late, canonical, sub, made, kept,
		{Canonical: localWords(name)}} // want "namechannels: ir.Naming.Canonical is filled by something other than a call into ir or compilers/compile; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords"
}

func fill(s *string) { *s = "" }
