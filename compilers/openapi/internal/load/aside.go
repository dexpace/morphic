// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"slices"
	"sync"

	yaml "go.yaml.in/yaml/v3"
)

// aside holds each node the load changed so that the parser reads the tree as
// the rule table judged it, with the node as it was, until restore. The model
// faults on a few shapes the table reports and reads as left out
// (docs/parser-library.md#nil-populate-panic): those are set aside for every
// build of the model, from settle until the references' targets are
// validated, and then put back, so every later reader sees the source as
// written.
type aside struct {
	mu      sync.Mutex // a document a reference reads is readied on its reader
	changed []asWritten
}

// asWritten is a node the load changed, what it was before, and the change,
// which whileWritten makes again.
type asWritten struct {
	node   *yaml.Node
	was    yaml.Node
	change func()
}

// hide takes out of the mapping m each pair drop reports, until restore.
func (a *aside) hide(m *yaml.Node, drop func(key, val *yaml.Node) bool) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	kept := make([]*yaml.Node, 0, len(m.Content))
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !drop(m.Content[i], m.Content[i+1]) {
			kept = append(kept, m.Content[i], m.Content[i+1])
		}
	}
	if len(kept) == len(m.Content) {
		return
	}
	a.keep(m, func() { a.hide(m, drop) })
	m.Content = kept
}

// spoil has n, a null, read as an empty sequence until restore.
func (a *aside) spoil(n *yaml.Node) {
	a.keep(n, func() { a.spoil(n) })
	*n = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, Line: n.Line, Column: n.Column}
}

// standIn has the mapping m read as a Reference Object whose $ref is an empty
// sequence, as spoil leaves one, until restore.
func (a *aside) standIn(m *yaml.Node) {
	a.keep(m, func() { a.standIn(m) })
	m.Content = []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "$ref", Line: m.Line, Column: m.Column},
		{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, Line: m.Line, Column: m.Column},
	}
}

// keep records n as it is now, so restore puts it back, and change, the
// change about to be made to it.
func (a *aside) keep(n *yaml.Node, change func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.changed = append(a.changed, asWritten{node: n, was: *n, change: change})
}

// restore puts back every node the aside changed, newest first, and then
// holds nothing, so a second restore changes nothing.
func (a *aside) restore() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range slices.Backward(a.changed) {
		*c.node = c.was
	}
	a.changed = nil
}

// whileWritten puts back every node the aside changed, runs read over the tree
// as written, and then makes each change again, oldest first: what read
// leaves refused reads as left out (nodeview.Unread) whether or not a change
// covers it, and a change to a node read turned is made to that node as it
// now is.
func (a *aside) whileWritten(read func()) {
	a.mu.Lock()
	changes := make([]func(), len(a.changed))
	for i, c := range a.changed {
		changes[i] = c.change
	}
	a.mu.Unlock()
	a.restore()
	read()
	for _, change := range changes {
		change()
	}
}
