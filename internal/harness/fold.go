// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/dexpace/morphic/ir"
)

// folded returns doc with its decorators (ir.Decorated) left out of Types, and
// the marshalers that write each reference to one as a reference to the type
// it stands for, what the chain holds beside it, and at a carrier what
// carried.write merges. Two spellings of one use then encode alike. An ID
// naming a decorator is written as the type's alone, so what a decorator holds
// is compared only where a reference names it. It is a second implementation
// of the emitter's collapse (docs/emitters/refine/decorators.md), until
// emitters/refine holds that one, which replaces it.
func folded(doc *ir.Document) (*ir.Document, *json.Marshalers) {
	uses := decoratorUses(doc.Types)
	if len(uses) == 0 {
		return doc, nil
	}
	return withoutTypes(doc, slices.Sorted(maps.Keys(uses))), json.JoinMarshalers(
		json.MarshalToFunc(func(enc *jsontext.Encoder, id ir.TypeID) error {
			u, ok := uses[id]
			if !ok {
				return errors.ErrUnsupported
			}
			return enc.WriteToken(jsontext.String(string(u.target)))
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, r ir.TypeRef) error {
			u, ok := uses[r.Target]
			if !ok {
				return errors.ErrUnsupported
			}
			r.Target = u.target
			return json.MarshalEncode(enc, foldedRef{Ref: plainRef(r), Use: u.notes.written()})
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, p ir.Property) error {
			c := carried[plainProperty]{Carrier: plainProperty(p)}
			return c.write(enc, uses, slots{&c.Carrier.Type, &c.Carrier.Docs, &c.Carrier.Deprecation,
				&c.Carrier.Examples, true})
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, h ir.Header) error {
			c := carried[plainHeader]{Carrier: plainHeader(h)}
			return c.write(enc, uses, slots{&c.Carrier.Type, &c.Carrier.Docs, &c.Carrier.Deprecation,
				&c.Carrier.Examples, true})
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, p ir.Parameter) error {
			c := carried[plainParameter]{Carrier: plainParameter(p)}
			return c.write(enc, uses, slots{&c.Carrier.Type, &c.Carrier.Docs, &c.Carrier.Deprecation,
				&c.Carrier.Examples, true})
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, v ir.Variant) error {
			c := carried[plainVariant]{Carrier: plainVariant(v)}
			return c.write(enc, uses, slots{&c.Carrier.Type, &c.Carrier.Docs, &c.Carrier.Deprecation,
				&c.Carrier.Examples, false})
		}),
	)
}

// withoutTypes returns doc less the types at ids, removed through an
// ir.Editor, which leaves doc as it was. What still names a removed type is
// the fold's marshalers' to rewrite. An ID doc holds no type at, which the
// editor refuses, is the harness's mistake, and a panic.
func withoutTypes(doc *ir.Document, ids []ir.TypeID) *ir.Document {
	e := ir.NewEditor(doc)
	for _, id := range ids {
		e.RemoveType(id)
	}
	own, refused := e.Finish()
	if len(refused) > 0 {
		panic(fmt.Sprintf("harness: leave the decorators out: %s", refused[0].Message))
	}
	return own
}

// use is what a decorator stands for: the first type along its Base chain that
// is no decorator, the chain's annotations, merged nearest first, and lent, the
// Docs and Deprecation of that type, which a property, parameter or header
// takes after the chain's, as a reference to the type lends them
// (docs/ir/annotations.md#what-a-reference-carries-down).
type use struct {
	target ir.TypeID
	notes  annotations
	lent   annotations
}

// decoratorUses returns, for each decorator of types, what it stands for. A
// chain that returns to a decorator it passed collapses nowhere, so it is left
// out and its nodes kept.
func decoratorUses(types ir.TypeRegistry) map[ir.TypeID]use {
	uses := map[ir.TypeID]use{}
	for id, td := range types {
		if _, ok := ir.Decorated(td); !ok {
			continue
		}
		if u, ok := chainOf(types, id); ok {
			uses[id] = u
		}
	}
	return uses
}

// chainOf walks from the decorator id along Base to the first type that is no
// decorator, merging each decorator's annotations on the way. A chain of
// distinct decorators reaches one within a step per type and one more, so a
// walk that has not is going round a cycle, and returns false.
func chainOf(types ir.TypeRegistry, id ir.TypeID) (use, bool) {
	var notes annotations
	at := id
	for range len(types) + 1 {
		base, ok := ir.Decorated(types[at])
		if !ok {
			return use{target: at, notes: notes, lent: lentBy(types[at])}, true
		}
		notes = notes.then(notesOf(types[at].Common()))
		at = base.Target
	}
	return use{}, false
}

// lentBy returns the annotations td lends a carrier naming it: its Docs and
// Deprecation, and none of a type the registry does not hold. Its Examples
// describe the type and stay on it (docs/compilers/openapi.md#schema-examples).
func lentBy(td ir.TypeDef) annotations {
	if td == nil {
		return annotations{}
	}
	c := td.Common()
	return annotations{docs: c.Docs, deprecation: c.Deprecation}
}

// annotations are the fields a decorator shares with a use site, merged along
// a chain: each field of Docs and of Deprecation, and Availability, from the
// first that sets one, Examples whole from the nearest, and Unmodeled the
// union by name.
type annotations struct {
	docs         ir.Docs
	deprecation  *ir.Deprecation
	examples     []ir.Example
	availability *ir.Availability
	unmodeled    map[string][]ir.UnmodeledEntry
}

// notesOf returns the annotations a decorator's c holds.
func notesOf(c *ir.TypeCommon) annotations {
	return annotations{docs: c.Docs, deprecation: c.Deprecation, examples: c.Examples, availability: c.Availability,
		unmodeled: entryLists(c.Unmodeled)}
}

// entryLists returns u's entries as one-entry lists, by name.
func entryLists(u ir.Unmodeled) map[string][]ir.UnmodeledEntry {
	if len(u) == 0 {
		return nil
	}
	out := make(map[string][]ir.UnmodeledEntry, len(u))
	for name, e := range u {
		out[name] = []ir.UnmodeledEntry{e}
	}
	return out
}

// then returns a merged with b, a nearer: a field a sets is a's, and an
// unmodeled name both hold keeps a's entries, then b's.
func (a annotations) then(b annotations) annotations {
	out := a
	out.docs = mergeDocs(a.docs, b.docs)
	out.deprecation = mergeDeprecation(a.deprecation, b.deprecation)
	if len(out.examples) == 0 {
		out.examples = b.examples
	}
	if out.availability == nil {
		out.availability = b.availability
	}
	if len(b.unmodeled) > 0 {
		out.unmodeled = make(map[string][]ir.UnmodeledEntry, len(a.unmodeled)+len(b.unmodeled))
		maps.Copy(out.unmodeled, a.unmodeled)
		for name, entries := range b.unmodeled {
			out.unmodeled[name] = append(append([]ir.UnmodeledEntry(nil), out.unmodeled[name]...), entries...)
		}
	}
	return out
}

// mergeDocs returns a's docs with each field a leaves empty taken from b, a
// Description and its Refs as one, since each Ref indexes its own description.
func mergeDocs(a, b ir.Docs) ir.Docs {
	if a.Summary == "" {
		a.Summary = b.Summary
	}
	if a.Description == "" && len(a.Refs) == 0 {
		a.Description, a.Refs = b.Description, b.Refs
	}
	if len(a.ExternalDocs) == 0 {
		a.ExternalDocs = b.ExternalDocs
	}
	return a
}

// mergeDeprecation returns a's deprecation with each field a leaves empty
// taken from b, as the compiler fills a use site's from the schema it names
// (docs/ir/annotations.md#what-a-reference-carries-down), and the one set
// where the other is nil: a carrier is deprecated where either is.
func mergeDeprecation(a, b *ir.Deprecation) *ir.Deprecation {
	if a == nil || b == nil {
		return cmp.Or(a, b)
	}
	out := *a
	out.Message = cmp.Or(a.Message, b.Message)
	out.Since = cmp.Or(a.Since, b.Since)
	out.RemovalVersion = cmp.Or(a.RemovalVersion, b.RemovalVersion)
	out.RemovalDate = cmp.Or(a.RemovalDate, b.RemovalDate)
	if len(out.ReplacedBy) == 0 {
		out.ReplacedBy = b.ReplacedBy
	}
	return &out
}

// written is a as a reference's use member writes it, nil where a holds
// nothing: a decorator setting no annotation is a bare reference.
func (a annotations) written() *useMember {
	u := useMember{Docs: a.docs, Deprecation: a.deprecation, Examples: a.examples, Availability: a.availability,
		Unmodeled: a.unmodeledMember()}
	if reflect.ValueOf(u).IsZero() {
		return nil
	}
	return &u
}

// unmodeledMember is a's Unmodeled as written: an entry by name, as ir.Unmodeled
// writes one, and a list of the entries where a name repeats.
func (a annotations) unmodeledMember() map[string]any {
	if len(a.unmodeled) == 0 {
		return nil
	}
	out := make(map[string]any, len(a.unmodeled))
	for name, entries := range a.unmodeled {
		if len(entries) == 1 {
			out[name] = entries[0]
			continue
		}
		out[name] = entries
	}
	return out
}

// useMember is a folded reference's use member: the annotations of the chain
// it names, which no carrier holds.
type useMember struct {
	Docs         ir.Docs          `json:"docs"`
	Deprecation  *ir.Deprecation  `json:"deprecation,omitzero"`
	Examples     []ir.Example     `json:"examples,omitempty"`
	Availability *ir.Availability `json:"availability,omitzero"`
	Unmodeled    map[string]any   `json:"unmodeled,omitempty"`
}

// The methodless copies the fold writes through, so writing one reaches none
// of its own marshalers again.
type (
	plainRef       ir.TypeRef
	plainProperty  ir.Property
	plainHeader    ir.Header
	plainParameter ir.Parameter
	plainVariant   ir.Variant
)

// foldedRef is a reference to a decorator: its own members, the target
// replaced, and the chain's annotations beside them.
type foldedRef struct {
	Ref plainRef   `json:",embed"`
	Use *useMember `json:"use,omitzero"`
}

// carried is a carrier whose type names a decorator: its own members, the
// chain's Docs, Deprecation and Examples merged in, and Decorator, what the
// chain holds that the collapse leaves on it.
type carried[T any] struct {
	Carrier   T           `json:",embed"`
	Decorator *keptBeside `json:"decorator,omitzero"`
}

// keptBeside is a chain's Availability and Unmodeled, which no carrier takes,
// so a carrier holding them differs from one naming a decorator that does.
type keptBeside struct {
	Availability *ir.Availability `json:"availability,omitzero"`
	Unmodeled    map[string]any   `json:"unmodeled,omitempty"`
}

// slots point at a carrier's reference and the fields the chain merges into,
// and say whether the carrier takes what the chain's type lends: a union
// variant takes none of it, as the compiler gives a variant none.
type slots struct {
	ref         *ir.TypeRef
	docs        *ir.Docs
	deprecation **ir.Deprecation
	examples    *[]ir.Example
	lends       bool
}

// write writes c with the chain its reference names folded in as the
// collapse folds it, "The decorators' annotations fold onto the carrier
// holding the reference — a property, a parameter, a header or a union
// variant, each of which has a field for all three"
// (docs/emitters/refine/decorators.md#decorators-collapse-into-their-references):
// the three its list and steps.md's CollapseAliases row name, Docs,
// Deprecation and Examples, the carrier's own first and what the chain's type
// lends last. It declines a carrier naming no decorator.
func (c *carried[T]) write(enc *jsontext.Encoder, uses map[ir.TypeID]use, s slots) error {
	u, ok := uses[s.ref.Target]
	if !ok {
		return errors.ErrUnsupported
	}
	own := annotations{docs: *s.docs, deprecation: *s.deprecation, examples: *s.examples}
	merged := own.then(u.notes)
	if s.lends {
		merged = merged.then(u.lent)
	}
	s.ref.Target = u.target
	*s.docs, *s.deprecation, *s.examples = merged.docs, merged.deprecation, merged.examples
	if u.notes.availability != nil || len(u.notes.unmodeled) > 0 {
		c.Decorator = &keptBeside{Availability: u.notes.availability, Unmodeled: u.notes.unmodeledMember()}
	}
	return json.MarshalEncode(enc, c)
}
