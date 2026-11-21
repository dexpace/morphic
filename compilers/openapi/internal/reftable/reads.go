// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reftable

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
	held   map[string]map[DocID]bool
	barred map[ask]error
	judged map[ask]bool
}

// newReads returns a record of nothing asked.
func newReads() *reads {
	return &reads{held: map[string]map[DocID]bool{}, barred: map[ask]error{}, judged: map[ask]bool{}}
}

// hold notes that the document from asked for name, held back for the
// pass's drain.
func (r *reads) hold(name string, from DocID) {
	if r.held[name] == nil {
		r.held[name] = map[DocID]bool{}
	}
	r.held[name][from] = true
}

// unread reports whether a read of name asked for by from would be the
// first: neither the document's read nor this asker's refusal is known.
func (r *reads) unread(docsValue *docs, name, from string) bool {
	_, barred := r.barred[ask{name, from}]
	return !barred && docsValue.unread(name)
}

// open returns the document named name, as the document from may read it:
// read through Config.Open the first time it is asked for, and judged by
// asking it again for each asker read from the network where another read it
// first, or failed to, since what such a document may reach turns on where
// both were read from, and a failure says what lies at an address. A refusal
// to from alone is kept for from alone.
func (r *reads) open(docsValue *docs, name, from string) (DocID, error) {
	askValue := ask{name, from}
	if err, ok := r.barred[askValue]; ok {
		return 0, err
	}
	_, opened := docsValue.byName[name]
	_, failed := docsValue.failed[name]
	if (opened || failed) && OverNetwork(from) && !r.judged[askValue] {
		r.judged[askValue] = true
		if err := docsValue.judge(name, from); err != nil {
			r.barred[askValue] = err
			return 0, err
		}
	}
	r.judged[askValue] = true
	id, err := docsValue.open(name, from)
	if barred := (*BarredError)(nil); errors.As(err, &barred) {
		r.barred[askValue] = err
	}
	return id, err
}

// admits reports whether the document from may read the document decl,
// which declares a URI from asks for: the source and from itself always,
// any other where from is not read from the network or Config.Open judges
// that it may, as a declaration answers only an asker that could read it.
func (r *reads) admits(docsValue *docs, decl, from DocID) bool {
	if decl == Source || decl == from || !OverNetwork(docsValue.name(from)) {
		return true
	}
	_, err := r.open(docsValue, docsValue.name(decl), docsValue.name(from))
	return err == nil
}

// drain reads the names the pass held, in two tiers, and forgets them: first
// every file, a path or a file URI, which costs no request, then, once what
// every document open declares is read (identifyAll), every URL no document
// open declares to the document that asked for it, as an identifier is not a
// location to request. Each is read for each document that asked, in one
// order. A failure is kept for the hop that asks again (docs.open,
// reads.open).
func (r *reads) drain(docsValue *docs, identifyAll func(), declares func(uri string, from DocID) bool) {
	var urls []string
	for _, name := range slices.Sorted(maps.Keys(r.held)) {
		if OverNetwork(name) {
			urls = append(urls, name)
			continue
		}
		r.readFor(docsValue, name, func(DocID) bool { return true })
	}
	identifyAll()
	for _, name := range urls {
		r.readFor(docsValue, name, func(from DocID) bool { return !declares(name, from) })
	}
	identifyAll()
	clear(r.held)
}

// readFor reads name for each document that held it and wants says reads it.
func (r *reads) readFor(docsValue *docs, name string, wants func(from DocID) bool) {
	for _, from := range slices.Sorted(maps.Keys(r.held[name])) {
		if wants(from) {
			_, _ = r.open(docsValue, name, docsValue.name(from))
		}
	}
}
