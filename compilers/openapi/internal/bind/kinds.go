// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"encoding/json/jsontext"
	"strings"
)

// Kind is what the OpenAPI grammar reads a position as, in the terms the
// nodes built beneath it are named in (Table.Kinds). An object or a list the
// grammar models and naming does not tell apart is KindOther.
type Kind uint8

const (
	// KindRaw is a position the model holds as raw YAML or as a value.
	KindRaw Kind = iota
	// KindOther is any other object or list the model holds.
	KindOther
	// KindComponents is the Components Object.
	KindComponents
	// KindSchemaMap is a map of schemas: a component section, or a schema's
	// properties, $defs or the like.
	KindSchemaMap
	// KindSchema is a Schema Object.
	KindSchema
	// KindOperation is an Operation Object.
	KindOperation
	// KindParameter is a Parameter Object.
	KindParameter
	// KindHeader is a Header Object.
	KindHeader
	// KindRequestBody is a Request Body Object.
	KindRequestBody
	// KindResponse is a Response Object.
	KindResponse
	// KindMediaType is a Media Type Object.
	KindMediaType
)

// kindOf is the Kind of a position read at s.
func kindOf(s state) Kind {
	switch s {
	case sRaw, sValue:
		return KindRaw
	case sComponents:
		return KindComponents
	case sSchemaMap:
		return KindSchemaMap
	case sSchema:
		return KindSchema
	case sOperation:
		return KindOperation
	case sParameter:
		return KindParameter
	case sHeader:
		return KindHeader
	case sRequestBody:
		return KindRequestBody
	case sResponse:
		return KindResponse
	case sMediaType:
		return KindMediaType
	default:
		return KindOther
	}
}

// Kinds returns how the grammar reads each position on the way to p from its
// document's root, the root's first: the source's own grammar from its root,
// and, at a position it reads as raw YAML, or in another document, the kind a
// reference read the position at, where one did (GitHub #981). A position
// written in the source is what its own position makes it, whatever a
// reference expects it to be; where references read one position as two
// kinds, the lesser counts, so the answer depends on no reading order.
func (t *Table) Kinds(p Position) []Kind {
	var out []Kind
	t.read(p, func(s state) { out = append(out, kindOf(s)) })
	return out
}

// Reading is how the grammar reads one position (Table.ReadingAt), so that a
// walk beneath it reads each position below without spelling out its pointer.
// The zero Reading reads raw YAML.
type Reading struct{ s state }

// ReadingAt returns how the grammar reads p, as Kinds reads the last position
// on the way to it. A pointer past the tokens Kinds reads, or a nil table,
// reads raw YAML.
func (t *Table) ReadingAt(p Position) Reading {
	if t == nil {
		return Reading{}
	}
	var r Reading
	if !t.read(p, func(s state) { r = Reading{s: s} }) {
		return Reading{}
	}
	return r
}

// Below returns how the grammar reads the position token names directly
// beneath r's. Unlike Kinds, it does not read a position as a reference landing
// there reads it: beneath raw YAML, such as an extension's value, everything
// is raw YAML.
func (r Reading) Below(token string) Reading { return Reading{s: step(r.s, token)} }

// Schema reports whether r reads a Schema Object.
func (r Reading) Schema() bool { return r.s == sSchema }

// Referable reports whether r reads an object a reference object can stand
// for, so a $ref written there names an object of that kind.
func (r Reading) Referable() bool { return referenceKinds[r.s] }

// read hands visit how the grammar reads each position on the way to p, the
// root's first, as Kinds documents, and reports whether it read every token of
// p's pointer rather than stopping at maxPointerTokens.
func (t *Table) read(p Position, visit func(state)) bool {
	s := sRaw
	if p.Doc == Source {
		s = sDocument
	}
	s = t.rooted(Position{Doc: p.Doc}, s)
	visit(s)
	pointer := string(p.Pointer)
	i := 0
	for tokens := 0; i < len(pointer) && tokens < maxPointerTokens; tokens++ {
		end := strings.IndexByte(pointer[i+1:], '/')
		if end < 0 {
			end = len(pointer)
		} else {
			end += i + 1
		}
		token := jsontext.Pointer(pointer[i:end]).LastToken()
		s = t.rooted(Position{Doc: p.Doc, Pointer: jsontext.Pointer(pointer[:end])}, step(s, token))
		visit(s)
		i = end
	}
	return i >= len(pointer)
}

// rooted returns s, the state the grammar reads at, or, where that is raw
// YAML, the state a reference read at as a root of its own, where one did.
func (t *Table) rooted(at Position, s state) state {
	if s != sRaw {
		return s
	}
	if r, ok := t.roots[at]; ok {
		return r
	}
	return s
}
