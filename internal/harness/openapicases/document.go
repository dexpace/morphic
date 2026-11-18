// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"slices"
)

// maxDepth bounds how deep a written document nests: deeper than any case
// writes, so passing it is the writer's defect.
const maxDepth = 32

// member is one member of a JSON object.
type member struct {
	name  string
	value any
}

// object is a JSON object whose members are written in their order, so no map
// iteration decides a document's text.
type object []member

// raw is JSON text written as it stands, such as a construct's value.
type raw string

// with returns o with each member of more set: one o names already takes its
// value in its place, and the rest follow in their order.
func (o object) with(more object) object {
	out := slices.Clone(o)
	for _, m := range more {
		i := slices.IndexFunc(out, func(n member) bool { return n.name == m.name })
		if i < 0 {
			out = append(out, m)
			continue
		}
		out[i] = m
	}
	return out
}

// encode returns v as indented JSON text, each object's members in order. A
// value no case writes, a name written twice, a raw value that is no JSON and
// a document deeper than maxDepth are errors.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	w := writer{enc: jsontext.NewEncoder(&buf, jsontext.WithIndent("  "))}
	w.write(v, 0)
	if w.err != nil {
		return nil, w.err
	}
	return buf.Bytes(), nil
}

// writer writes a document's tokens to enc, keeping the first error, after
// which it writes nothing.
type writer struct {
	enc *jsontext.Encoder
	err error
}

// write writes v, depth objects and arrays deep.
func (w *writer) write(v any, depth int) {
	if depth > maxDepth {
		w.fail(fmt.Errorf("openapicases: a document deeper than %d", maxDepth))
		return
	}
	switch v := v.(type) {
	case object:
		w.token(jsontext.BeginObject)
		for _, m := range v {
			w.token(jsontext.String(m.name))
			w.write(m.value, depth+1)
		}
		w.token(jsontext.EndObject)
	case []any:
		w.token(jsontext.BeginArray)
		for _, e := range v {
			w.write(e, depth+1)
		}
		w.token(jsontext.EndArray)
	case string:
		w.token(jsontext.String(v))
	case raw:
		if w.err == nil {
			w.err = w.enc.WriteValue(jsontext.Value(v))
		}
	default:
		w.fail(fmt.Errorf("openapicases: no JSON for a %T", v))
	}
}

// token writes t, unless an error came first.
func (w *writer) token(t jsontext.Token) {
	if w.err == nil {
		w.err = w.enc.WriteToken(t)
	}
}

// fail keeps err if it is the first error.
func (w *writer) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}
