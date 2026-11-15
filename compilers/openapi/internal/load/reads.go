// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"errors"
	"fmt"
	"sync"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// externalReads is what the external-document reader of one compile has read:
// each prepared document's released tree and the bytes it came from, by the
// key it was prepared under, and the answer each document's one read got (see
// once). It is safe for concurrent use.
type externalReads struct {
	mu    sync.Mutex
	self  sourceDocument
	trees map[string]*yaml.Node
	data  map[string][]byte
	// read is what reading each prepared document found beside its tree.
	read map[string]doctree.Document
	// answers holds each document's read by the key it was read under, and
	// documents charges each read to the document budget.
	answers   map[string]*reading
	documents budget.Budget
}

// newExternalReads returns a record of reads beside self, holding none, whose
// reads are held to documents, the document budget.
func newExternalReads(self sourceDocument, documents budget.Limit) *externalReads {
	return &externalReads{self: self, trees: map[string]*yaml.Node{}, data: map[string][]byte{},
		read:    map[string]doctree.Document{},
		answers: map[string]*reading{}, documents: budget.New(documents)}
}

// answer is how a read was answered: the error it failed with, or the body
// read, the spelling the document was prepared under, the address space it
// was read from and the most private one it can have dialled
// (compilers.DocumentRead), which a judgement at another reach can state where
// the read stated none (externalReads.verdict).
type answer struct {
	body     []byte
	err      error
	prepared string
	space    compilers.AddressSpace
	bound    compilers.AddressSpace
	// reach is the reach the read was asked under, where read says one was
	// made: an answer no read gave, such as the document budget's refusal,
	// is every asker's.
	reach compilers.AddressSpace
	read  bool
}

// reading is one document's read: the answer it got, set once, or, while it
// has none, the widest reach a read of it was refused to its asker under.
type reading struct {
	mu      sync.Mutex
	done    bool
	answer  answer
	charged bool
	barred  error
	// barredAt is the widest reach barred was given under.
	barredAt compilers.AddressSpace
	// verdicts holds, once it is read with no bound stated, the judgement of
	// it at each reach an asker was judged at (verdict).
	verdicts map[compilers.AddressSpace]error
}

// once returns the answer the document under key got when it was read, as an
// asker bounded by reach is answered. The first read of the compile, charged
// to the document budget, reads it with read, and refuses it unread past the
// budget; every later ask gets the same answer, so a document is read and
// charged once however many references name it. A read refused to its asker
// alone (bind.BarredError) is no answer: an asker the refusal covers, bounded
// no wider, is refused again unread, and a wider one reads.
func (r *externalReads) once(key string, reach compilers.AddressSpace, read func() answer) answer {
	rd := r.reading(key)
	rd.mu.Lock()
	defer rd.mu.Unlock()
	switch {
	case rd.done:
		return rd.answer
	case rd.barred != nil && !wider(reach, rd.barredAt):
		return answer{err: rd.barred}
	}
	// Set first, so a read that panics leaves a failure rather than an empty
	// document.
	rd.done, rd.answer = true, answer{err: refused(key, "its read did not finish")}
	if !rd.charged {
		rd.charged = true
		if err := r.charge(key); err != nil {
			rd.answer = answer{err: err}
			return rd.answer
		}
	}
	a := read()
	if barred := (*bind.BarredError)(nil); errors.As(a.err, &barred) {
		rd.done, rd.barred, rd.barredAt = false, a.err, reach
		return a
	}
	rd.answer = a
	return a
}

// reading returns the read of the document under key, made or not.
func (r *externalReads) reading(key string) *reading {
	r.mu.Lock()
	defer r.mu.Unlock()
	rd, ok := r.answers[key]
	if !ok {
		rd = &reading{}
		r.answers[key] = rd
	}
	return rd
}

// verdict returns the refusal of the document under key, read with no bound
// stated, to an asker bounded by reach, or nil, by judge, a read of it at
// reach: at most once per reach, a refusal kept for every asker at it. A
// judgement that passes states the document's bound, by which every later
// asker is judged unread (external.admits).
func (r *externalReads) verdict(key string, reach compilers.AddressSpace,
	judge func() (compilers.AddressSpace, error),
) error {
	rd := r.reading(key)
	rd.mu.Lock()
	defer rd.mu.Unlock()
	if rd.answer.bound != compilers.SpaceUnstated {
		return beyond(key, rd.answer.bound, reach)
	}
	if err, judged := rd.verdicts[reach]; judged {
		return err
	}
	bound, err := judge()
	if err == nil {
		rd.answer.bound = bound
		return nil
	}
	if rd.verdicts == nil {
		rd.verdicts = map[compilers.AddressSpace]error{}
	}
	rd.verdicts[reach] = err
	return err
}

// wider reports whether a read bounded by reach may dial further than one
// bounded by than: unbounded, or bounded by a more private space.
func wider(reach, than compilers.AddressSpace) bool {
	return reach == compilers.SpaceUnstated || reach > than
}

// spaceOf returns the space the document under key was read from, or
// SpaceUnstated where it was read from none or is not read.
func (r *externalReads) spaceOf(key string) compilers.AddressSpace {
	r.mu.Lock()
	rd, ok := r.answers[key]
	r.mu.Unlock()
	if !ok {
		return compilers.SpaceUnstated
	}
	rd.mu.Lock()
	defer rd.mu.Unlock()
	return rd.answer.space
}

// adopt records the tree and the bytes prepared under prepared under key too,
// another spelling of one document, unless key has a tree already.
func (r *externalReads) adopt(key, prepared string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, held := r.trees[key]; held {
		return
	}
	if tree, ok := r.trees[prepared]; ok {
		r.trees[key], r.data[key], r.read[key] = tree, r.data[prepared], r.read[prepared]
	}
}

// charge charges the read of the document under key to the document budget,
// and returns the document's refusal where the budget has no room for it.
func (r *externalReads) charge(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.documents.Spend(1) {
		return nil
	}
	return refused(key, fmt.Sprintf("it is past the %d-document budget", r.documents.Limit().Cap()))
}

// exhausted reports the document budget crossed, once, at the source.
func (r *externalReads) exhausted(place provenance.Doc) []ir.Diagnostic {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.documents.Exhausted() {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.BudgetExceeded, place.At(""),
		"external references name more documents than the %d-document budget; "+
			"the $refs into the rest are reported unresolved", r.documents.Limit().Cap())}
}

// recordTree notes a document prepared under key from data.
func (r *externalReads) recordTree(key string, doc doctree.Document, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trees[key], r.read[key] = doc.Root, doc
	r.data[key] = data
}

// preparedFor returns the tree prepared for the document name names, or false
// for none. A URL is named as it is located, one spelling for every way a
// reference writes it (resolve.Locate), so it is read under that name.
func (r *externalReads) preparedFor(name string) (*yaml.Node, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tree, ok := r.trees[name]
	return tree, ok
}
