// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"slices"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// defsRoot is the position a schema's definitions sit at, and defsPrefix opens
// every pointer the $defs rule reads.
const (
	defsRoot   jsontext.Pointer = "/$defs"
	defsPrefix                  = string(defsRoot) + "/"
)

// isDefs reports whether pointer is one the $defs rule reads, from where the
// $ref spelling it is written rather than from its document's root alone.
func isDefs(pointer jsontext.Pointer) bool {
	return strings.HasPrefix(string(pointer), defsPrefix)
}

// definitions reads "#/$defs/..." pointers by the $defs rule, over a table's
// documents and within its budget.
type definitions struct {
	docs *docs
	work *work
}

// definition is where a "#/$defs/..." pointer written at site, reached along
// path, in its document's own resource, lands, by the rule
// docs/compilers/openapi.md#references states: the position it names from the
// document's root, else the definitions of the nearest object above site
// holding "$defs" in the model, whatever was read before
// (docs/parser-library.md#defs-order, docs/parser-library.md#root-defs). A
// schema's own answer only where an $id makes it a resource, which resolve
// reads instead; where schema says site's own hold the key, the reason says
// so. A read the merge bound stopped answers MergeBound.
func (d definitions) definition(site Position, path []*yaml.Node, pointer jsontext.Pointer, schema bool,
) (reading, Reason) {
	r, reason := d.docs.read(site.Doc, pointer)
	switch {
	case reason == NoReason && answers(r):
		return r, NoReason
	case reason == MergeBound:
		return reading{}, MergeBound
	}
	r, why := d.ancestorDefinition(site, path, pointer)
	if key, single := singleKey(pointer); schema && single && (why == NoDefinition || why == RawDefinition) &&
		d.holds(site.Doc, path[len(path)-1], []string{"$defs", key}) {
		why = OwnDefinition // the nearer definition left unread
	}
	return r, why
}

// ancestorDefinition is the definition the nearest object above site that
// holds "$defs" in the model gives pointer, read on into raw YAML where the
// model leaves it, as the parser's resolver reads it (GitHub #997). The root
// is not such an object: definition has read it already. A read the merge
// bound stops makes it MergeBound, as a farther object would answer only
// where that one holds nothing. Where none answers and raw YAML above site
// holds the path, the reason is RawDefinition.
func (d definitions) ancestorDefinition(site Position, path []*yaml.Node, pointer jsontext.Pointer) (reading, Reason) {
	if !d.docs.list[site.Doc].grammar {
		return reading{}, NoDefinition
	}
	tokens := slices.Collect(site.Pointer.Tokens())
	states := make([]state, len(tokens)+1)
	states[0] = sDocument
	for i, t := range tokens {
		states[i+1] = step(states[i], t)
	}
	rest := slices.Collect(pointer.Tokens())
	why := NoDefinition
	for i := len(tokens) - 1; i >= 1; i-- {
		if !d.work.spend(1) {
			break
		}
		if !modelled(states[i]) {
			if why == NoDefinition && d.holds(site.Doc, path[i], rest) {
				why = RawDefinition
			}
			continue
		}
		if !modelled(step(states[i], "$defs")) {
			continue
		}
		n, past := d.below(site.Doc, path[i], rest)
		if r := (reading{node: n, state: stateOf(states[i], rest)}); n != nil && answers(r) {
			r.at = Position{Doc: site.Doc, Pointer: jsontext.Pointer(prefix(site.Pointer, i)) + pointer}
			return r, NoReason
		}
		if past {
			return reading{}, MergeBound
		}
	}
	return reading{}, why
}

// below is the node tokens name from n in document id, or nil, and whether the
// read stopped at a mapping merging past the view's bound (see absent).
func (d definitions) below(id DocID, n *yaml.Node, tokens []string) (*yaml.Node, bool) {
	for _, t := range tokens {
		if !d.work.spend(1) {
			return nil, false
		}
		next := d.docs.child(id, n, t)
		if next == nil {
			return nil, d.docs.absent(id, n) == MergeBound
		}
		n = next
	}
	return n, false
}

// holds reports whether tokens name a node read as a schema below n in
// document id.
func (d definitions) holds(id DocID, n *yaml.Node, tokens []string) bool {
	held, _ := d.below(id, n, tokens)
	return schemaShaped(held)
}

// answers reports whether a definition read at r answers its pointer: a
// schema of the model, which the parser builds whatever the YAML there holds,
// or raw YAML shaped as one.
func answers(r reading) bool {
	switch roleOf(r.state) {
	case Schema:
		return r.node != nil
	case Raw:
		return schemaShaped(r.node)
	default:
		return false
	}
}

// prefix is the pointer to pointer's first n tokens: pointer up to the '/'
// opening the token after them, since an escaped token holds none.
func prefix(pointer jsontext.Pointer, n int) string {
	s := string(pointer)
	for i := range len(s) {
		if s[i] != '/' {
			continue
		}
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// singleKey is the one key "/$defs/key" names, and whether pointer names one
// key with no path beyond it.
func singleKey(pointer jsontext.Pointer) (string, bool) {
	ts := slices.Collect(pointer.Tokens())
	if len(ts) != 2 {
		return "", false
	}
	return ts[1], true
}
