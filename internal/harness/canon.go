// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/ir"
)

// maxCanonicalDepth bounds the canonical form's descent: through the encoded
// document, and through the owners of each digest computed on the way, whose
// rewrite continues the descent that met the ID. A document nests its source's
// values a few levels below its own, and a digest follows owned IDs downward
// through that source, so the bound is a multiple of the source's.
const maxCanonicalDepth = 4 * maxReverseDepth

// errTooDeep says a canonical form would nest past maxCanonicalDepth.
var errTooDeep = errors.New("canonical form nests too deep")

// unorderedMark names the one member of the object markedArray writes.
const unorderedMark = "\x00unordered"

// markedArray writes elems as they stand, inside an object marking them as a
// collection whose order the comparison forgives, so the rewrite sorts them
// once it has rewritten them. No member of the IR is so named.
func markedArray(elems [][]byte) []byte {
	return append(append([]byte(`{"\u0000unordered":`), array(elems)...), '}')
}

// canonical is a document's canonical form: its lines, and its shape's.
type canonical struct{ document, shape []string }

// canonicalForm is doc as the sequence-order oracle compares it, where seqs
// are the sequences reversed, in doc's source's coordinates, and ties the
// erased pointers of the reports that moved. Every string an index of a
// reversed sequence reaches is erased, and an ID a node owns is told apart by
// that node's digest. A node at or beneath a tie is written as the tie, and a
// collection orderFree lists, mixins, requires and a kept allOf or required
// array are sorted.
func canonicalForm(doc *ir.Document, seqs sequences, ties []string) (canonical, error) {
	encoded, err := encodeComparable(doc, markedArray)
	if err != nil {
		return canonical{}, err
	}
	r := rewriter{seqs: seqs, ties: ties, owners: map[string][]map[string]any{},
		digests: map[string]string{}, visiting: map[string]bool{}}
	tree := r.decode(encoded)
	r.index(tree, 0)
	out := jsontext.Value(r.rewrite(tree, 0, 0))
	r.fail(out.Indent(jsontext.WithIndent("  ")))
	shape := r.shape(doc)
	return canonical{document: lines(out), shape: shape}, r.err
}

// number is a JSON number's text, which the rewrite writes as it stands.
type number string

// rawNumber decodes a JSON number as its text, which no Go number type holds
// whole.
func rawNumber(dec *jsontext.Decoder, v *any) error {
	if dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	raw, err := dec.ReadValue()
	*v = number(raw)
	return err
}

// placement is what a node's place tells the rewrite about it.
type placement uint8

const (
	// sortElements: an array whose order the comparison forgives.
	sortElements placement = 1 << iota
	// relativeNames: an object whose member names are pointers relative to
	// the node holding it, as an unmodeled map's are.
	relativeNames
	// noIdentity: a node to write without its id and provenance members,
	// whatever its depth beneath the node first so placed.
	noIdentity
)

// rewriter is the state of one canonicalForm: the sequences and tie sites it
// reads, the owners of each ID erasing changes, the digests computed so far,
// the IDs whose digest is being computed, and the first error met.
type rewriter struct {
	seqs     sequences
	ties     []string
	owners   map[string][]map[string]any
	digests  map[string]string
	visiting map[string]bool
	err      error
}

// fail records err, unless an earlier error is recorded.
func (r *rewriter) fail(err error) { r.err = cmp.Or(r.err, err) }

// decode reads an encoding into a tree of objects, arrays, strings, numbers
// as their text, booleans and nulls.
func (r *rewriter) decode(encoded []byte) any {
	var tree any
	r.fail(json.Unmarshal(encoded, &tree, json.WithUnmarshalers(json.UnmarshalFromFunc(rawNumber))))
	return tree
}

// index records, below v, each owner of an ID: an object holding it as its id
// member, where erasing changes the ID.
func (r *rewriter) index(v any, depth int) {
	if depth > maxCanonicalDepth {
		r.fail(errTooDeep)
		return
	}
	switch v := v.(type) {
	case map[string]any:
		if id, ok := v["id"].(string); ok && erase(id, r.seqs) != id {
			r.owners[id] = append(r.owners[id], v)
		}
		for _, member := range v {
			r.index(member, depth+1)
		}
	case []any:
		for _, element := range v {
			r.index(element, depth+1)
		}
	default:
		// A scalar owns nothing.
	}
}

// rewrite returns v's canonical encoding, compact, as its place says.
func (r *rewriter) rewrite(v any, as placement, depth int) []byte {
	if depth > maxCanonicalDepth {
		r.fail(errTooDeep)
		return []byte("null")
	}
	switch v := v.(type) {
	case map[string]any:
		return r.object(v, as, depth)
	case []any:
		return r.array(v, as, depth)
	case string:
		return r.quote(r.text(v, depth))
	case number:
		return []byte(v)
	default:
		b, err := json.Marshal(v) // a boolean or a null
		r.fail(err)
		return b
	}
}

// object rewrites an object: a marked collection as a sorted array, one at or
// beneath a tie site as the tie, and any other member by member, an id erased
// alone, sorted by name and then by value, since erased names can collide.
func (r *rewriter) object(o map[string]any, as placement, depth int) []byte {
	if elems, ok := o[unorderedMark].([]any); ok && len(o) == 1 {
		return r.array(elems, as|sortElements, depth)
	}
	pointer := provenancePointer(o)
	if erased := erase(pointer, r.seqs); as&noIdentity == 0 && r.tied(erased) {
		return slices.Concat([]byte(`{"reportedTie":`), r.quote(erased), []byte("}"))
	}
	type member struct{ name, value []byte }
	members := make([]member, 0, len(o))
	for name, v := range o {
		if as&noIdentity != 0 && (name == "id" || name == "provenance") {
			continue
		}
		key := r.text(name, depth)
		if as&relativeNames != 0 {
			key = eraseDecimals(key)
		}
		members = append(members, member{r.quote(key), r.member(name, v, as&noIdentity, r.seqs[pointer] > 0, depth)})
	}
	slices.SortFunc(members, func(a, b member) int {
		return cmp.Or(bytes.Compare(a.name, b.name), bytes.Compare(a.value, b.value))
	})
	out := []byte{'{'}
	for i, m := range members {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(append(append(out, m.name...), ':'), m.value...)
	}
	return append(out, '}')
}

// member rewrites the value of an object's member, as inherits from the
// object, where valueIsSequence says the object keeps a reversed sequence
// verbatim in its value member.
func (r *rewriter) member(name string, v any, as placement, valueIsSequence bool, depth int) []byte {
	if id, ok := v.(string); ok && name == "id" {
		return r.quote(erase(id, r.seqs))
	}
	switch {
	case name == "mixins", name == "requires", name == "allOf", name == "required",
		name == "value" && valueIsSequence:
		as |= sortElements
	case name == "unmodeled":
		as |= relativeNames
	}
	return r.rewrite(v, as, depth+1)
}

// array rewrites an array's elements, sorted when its place says so.
func (r *rewriter) array(elems []any, as placement, depth int) []byte {
	out := make([][]byte, len(elems))
	for i, e := range elems {
		out[i] = r.rewrite(e, as&noIdentity, depth+1)
	}
	if as&sortElements != 0 {
		return sortedArray(out)
	}
	return array(out)
}

// text is s erased, followed, where s is an ID with owners, by their digest.
func (r *rewriter) text(s string, depth int) string {
	erased := erase(s, r.seqs)
	if len(r.owners[s]) == 0 || r.visiting[s] {
		return erased
	}
	return erased + "#" + r.digest(s, depth)
}

// digest is the first six bytes, in hex, of the SHA-256 of the canonical
// encodings of id's owners, sorted and joined by newlines. An ID met while its
// own digest is computed is written erased alone, which ends a cycle.
func (r *rewriter) digest(id string, depth int) string {
	if d, ok := r.digests[id]; ok {
		return d
	}
	r.visiting[id] = true
	encodings := make([]string, 0, len(r.owners[id]))
	for _, o := range r.owners[id] {
		encodings = append(encodings, string(r.rewrite(o, 0, depth+1)))
	}
	delete(r.visiting, id)
	slices.Sort(encodings)
	sum := sha256.Sum256([]byte(strings.Join(encodings, "\n")))
	d := fmt.Sprintf("%x", sum[:6])
	r.digests[id] = d
	return d
}

// quote is s as a JSON string.
func (r *rewriter) quote(s string) []byte {
	b, err := jsontext.AppendQuote(nil, s)
	r.fail(err)
	return b
}

// tied reports whether an erased pointer is at or beneath a tie site.
func (r *rewriter) tied(erased string) bool {
	return slices.ContainsFunc(r.ties, func(site string) bool {
		return erased == site || strings.HasPrefix(erased, site) && strings.HasPrefix(erased[len(site):], "/")
	})
}

// provenancePointer is the pointer an object's provenance member holds, or
// empty where it holds none.
func provenancePointer(o map[string]any) string {
	p, _ := o["provenance"].(map[string]any)
	pointer, _ := p["pointer"].(string)
	return pointer
}

// eraseDecimals writes every all-decimal token of a relative pointer as "-".
func eraseDecimals(name string) string {
	tokens := strings.Split(name, "/")
	for i, t := range tokens {
		if decimal(t) {
			tokens[i] = "-"
		}
	}
	return strings.Join(tokens, "/")
}

// shape is a line per member of each model's flattened shape: the model's
// rewritten ID, the wire name, and what the member takes, stripped of the
// declaration's identity, or the tie its declaration sits at. A line more
// lists the model's undeclared and unflattened names where either holds any.
func (r *rewriter) shape(doc *ir.Document) []string {
	var out []string
	for id, t := range doc.Types {
		if _, ok := t.(*ir.Model); !ok {
			continue
		}
		f := ir.FlattenedProperties(doc.Types, id)
		model := r.text(string(id), 0)
		for _, p := range f.Properties {
			out = append(out, model+" member "+strconv.Quote(p.Property.WireName)+" "+r.memberShape(p))
		}
		if len(f.Undeclared)+len(f.Unflattened) == 0 {
			continue
		}
		unflattened := make([]string, len(f.Unflattened))
		for i, u := range f.Unflattened {
			unflattened[i] = r.text(string(u), 0)
		}
		out = append(out, fmt.Sprintf("%s undeclared %q unflattened %q", model,
			slices.Sorted(slices.Values(f.Undeclared)), slices.Sorted(slices.Values(unflattened))))
	}
	slices.Sort(out)
	return out
}

// memberShape is what a flattened member takes: whether it is required,
// secret and deprecated, and its declaration, rewritten without its identity,
// so two declarations saying the same are one.
func (r *rewriter) memberShape(p ir.FlatProperty) string {
	if erased := erase(string(p.Property.Provenance.Pointer), r.seqs); r.tied(erased) {
		return "reported tie at " + erased
	}
	encoded, err := json.Marshal(struct {
		Declaration *ir.Property    `json:"declaration"`
		Deprecation *ir.Deprecation `json:"deprecation"`
		Required    bool            `json:"required"`
		Secret      bool            `json:"secret"`
	}{p.Property, p.Deprecation, p.Required, p.Secret},
		json.Deterministic(true), json.WithMarshalers(orderFree(markedArray)), jsontext.ReorderRawObjects(true))
	r.fail(err)
	return string(r.rewrite(r.decode(encoded), noIdentity, 0))
}

// erase writes every index of a reversed sequence that s names as "-".
func erase(s string, seqs sequences) string {
	return rewriteIndices(s, seqs, func(int, int) string { return "-" })
}

// rebase writes every index i of a reversed sequence of length n that s names
// as n-1-i, which moves a string from the permuted source's coordinates to
// those of the source as written.
func rebase(s string, seqs sequences) string {
	return rewriteIndices(s, seqs, func(n, i int) string { return strconv.Itoa(n - 1 - i) })
}
