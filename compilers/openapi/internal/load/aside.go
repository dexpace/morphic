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

// asWritten is a node the load changed and what it was before.
type asWritten struct {
	node *yaml.Node
	was  yaml.Node
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
	a.keep(m)
	m.Content = kept
}

// spoil has n, a null, read as an empty sequence until restore.
func (a *aside) spoil(n *yaml.Node) {
	a.keep(n)
	*n = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle, Line: n.Line, Column: n.Column}
}

// keep records n as it is now, so restore puts it back.
func (a *aside) keep(n *yaml.Node) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.changed = append(a.changed, asWritten{node: n, was: *n})
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
