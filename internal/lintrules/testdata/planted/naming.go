package planted

import (
	"strings"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/ir"
)

func localWords(s string) string { return s }

func plantNaming(name string) []ir.Naming {
	declared := ir.Naming{Source: name, Canonical: localWords(name)}     // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	hinted := ir.Naming{Hint: localWords(name)}                          // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	carried := ir.Naming{Canonical: name}                                // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	wrapped := ir.Naming{Canonical: localWords(ir.CanonicalWords(name))} // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	var late ir.Naming
	late.Canonical = strings.ToLower(name) // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	late.Hint = localWords(name)           // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	var pair ir.Naming
	var count int
	pair.Canonical, count = compile.NamingFor(name).Canonical, 1 // want "ruleguard: a name channel is filled in a multiple assignment; fill it alone, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	count, pair.Hint = 2, localWords(name)                       // want "ruleguard: a name channel is filled in a multiple assignment; fill it alone, from compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	_, _ = pair, count
	byKey := map[string]ir.Naming{"k": {Hint: localWords(name)}} // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
	canonical := ir.Naming{Source: name, Canonical: ir.CanonicalWords(name)}
	sub := ir.Naming{Hint: compile.SubHint(name, "item")}
	made := compile.NamingFor(name)
	kept := ir.Naming{Source: name, Aliases: []string{name}}
	_ = byKey
	return []ir.Naming{declared, hinted, carried, wrapped, late, canonical, sub, made, kept,
		{Canonical: localWords(name)}} // want "ruleguard: a name channel is filled outside the framework; use compile.NamingFor, compile.NamingHint or ir.CanonicalWords (gocritic)"
}
