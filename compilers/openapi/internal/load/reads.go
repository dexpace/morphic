// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"fmt"
	"net/http"
	"sync"

	yaml "go.yaml.in/yaml/v3"

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
// read and the spelling the document was prepared under.
type answer struct {
	body     []byte
	err      error
	prepared string
}

// reading is one document's read: the answer it got, set once.
type reading struct {
	once   sync.Once
	answer answer
}

// once returns the answer the document under key got when it was read. The
// first ask of the compile reads it with read while the document budget
// allows one more document, and refuses it unread past that. Every later ask
// gets the same answer, so a document is read and charged once however many
// references name it.
func (r *externalReads) once(key string, read func() answer) answer {
	r.mu.Lock()
	rd, ok := r.answers[key]
	if !ok {
		rd = &reading{}
		r.answers[key] = rd
	}
	r.mu.Unlock()
	rd.once.Do(func() {
		// Set first, so a read that panics leaves a failure rather than an empty
		// document.
		rd.answer = answer{err: refused(key, "its read did not finish")}
		if err := r.charge(key); err != nil {
			rd.answer = answer{err: err}
			return
		}
		rd.answer = read()
	})
	return rd.answer
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
// for none: the one prepared under name, or under the key a request built from
// name spells, as a URL is read.
func (r *externalReads) preparedFor(name string) (*yaml.Node, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tree, ok := r.trees[name]; ok {
		return tree, true
	}
	req, err := http.NewRequest(http.MethodGet, name, nil)
	if err != nil {
		return nil, false
	}
	tree, ok := r.trees[req.URL.String()]
	return tree, ok
}
