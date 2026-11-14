// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package bind

import (
	"errors"
	"maps"
	"slices"
)

// BarredError is the failure of Config.Open for a read refused to the
// document that asked alone, such as one at an address more private than a
// document read from the network may reach: another asker may still read
// it, so it is never the document's answer.
type BarredError struct {
	// Why is the refusal, as a reference naming the document reports it.
	Why string
}

// Error implements error.
func (e *BarredError) Error() string { return e.Why }

// ask is one document's read of another: the name read, and the name of the
// document whose reference asked.
type ask struct{ name, from string }

// reads is who asked for what: each name a pass held back, with the
// documents that asked for it, and, for the whole binding, each read refused
// to its asker alone and each asker a document read from the network was
// judged for. A read barred to one asker is no answer of the document's
// (docs.failed), so whether a document is read turns on the set of documents
// naming it and never on which asked first.
type reads struct {
	held   map[string]map[string]bool
	barred map[ask]error
	judged map[ask]bool
}

// newReads returns a record of nothing asked.
func newReads() *reads {
	return &reads{held: map[string]map[string]bool{}, barred: map[ask]error{}, judged: map[ask]bool{}}
}

// hold notes that the document from asked for name, held back for the
// pass's drain.
func (r *reads) hold(name, from string) {
	if r.held[name] == nil {
		r.held[name] = map[string]bool{}
	}
	r.held[name][from] = true
}

// unread reports whether a read of name asked for by from would be the
// first: neither the document's read nor this asker's refusal is known.
func (r *reads) unread(d *docs, name, from string) bool {
	_, barred := r.barred[ask{name, from}]
	return !barred && d.unread(name)
}

// open returns the document named name, as the document from may read it:
// read through Config.Open the first time it is asked for, and judged by
// Config.Admits for each asker read from the network where another read it
// first, since what such a document may reach turns on where both were read
// from. A refusal to from alone is kept for from alone.
func (r *reads) open(d *docs, name, from string) (DocID, error) {
	k := ask{name, from}
	if err, ok := r.barred[k]; ok {
		return 0, err
	}
	if _, opened := d.byName[name]; opened && OverNetwork(from) && !r.judged[k] {
		r.judged[k] = true
		if err := d.judge(name, from); err != nil {
			r.barred[k] = err
			return 0, err
		}
	}
	r.judged[k] = true
	id, err := d.open(name, from)
	if barred := (*BarredError)(nil); errors.As(err, &barred) {
		r.barred[k] = err
	}
	return id, err
}

// admits reports whether the document from may read the document decl,
// which declares a URI from asks for: the source and from itself always,
// any other where from is not read from the network or Config.Admits judges
// that it may, as a declaration answers only an asker that could read it.
func (r *reads) admits(d *docs, decl, from DocID) bool {
	if decl == Source || decl == from || !OverNetwork(d.name(from)) {
		return true
	}
	_, err := r.open(d, d.name(decl), d.name(from))
	return err == nil
}

// drain reads every name the pass held, for each document that asked, in
// one order, and forgets them. A failure is kept for the hop that asks again
// (docs.open, reads.open).
func (r *reads) drain(d *docs) {
	for _, name := range slices.Sorted(maps.Keys(r.held)) {
		for _, from := range slices.Sorted(maps.Keys(r.held[name])) {
			_, _ = r.open(d, name, from)
		}
	}
	clear(r.held)
}
