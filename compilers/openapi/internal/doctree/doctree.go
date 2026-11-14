// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package doctree

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
)

// Document is one input's bytes read into the tree of its first document that
// holds content.
type Document struct {
	// Root is a document node, or a node of no kind for a stream holding none.
	Root *yaml.Node
	// Directive is the %YAML directive governing Root, or nil.
	Directive *Directive
	// Legacy is, in a document declaring YAML 1.1, each thing YAML 1.1 reads
	// otherwise than YAML 1.2, in document order.
	Legacy []Legacy
	// Rest is what the stream holds past Root; zero for a JSON text.
	Rest Tail
	// Nodes counts the nodes of Root's tree, each once, an alias as itself,
	// the document node included.
	Nodes int
}

// Directive is a `%YAML 1.<Minor>` directive and the line it is written on.
type Directive struct{ Minor, Line int }

// Legacy is one thing a document declaring YAML 1.1 writes that YAML 1.1 reads
// as another JSON value than YAML 1.2, at the node holding it: a plain scalar,
// with its two readings worded, or a character YAML 1.1 ends a line at, in the
// node's text or in a comment attached to it.
type Legacy struct {
	Node *yaml.Node
	// Was is YAML 1.1's reading and Is YAML 1.2's, worded: "the integer 420",
	// "a string". Both are empty for a line-break character.
	Was, Is string
	// Break is the line-break character, or 0 for a plain scalar.
	Break rune
	// InComment says the character is in a comment, not the node's text.
	InComment bool
	// Kinds says Was and Is name kinds alone, "an integer", for a scalar too
	// long to read as a value, whose text a finding does not quote.
	Kinds bool
}

// Tail is what a stream holds past the document taken. Its zero value is the
// answer for a stream with one document holding content.
type Tail struct {
	// Line and Column are where the first document past the one taken that
	// holds content begins; zero when none does. Lines count from one.
	Line, Column int
	// Dropped counts the documents past the one taken that hold content, up
	// to the bound on documents read.
	Dropped int
	// Capped says the stream holds a document past that bound, unread.
	Capped bool
	// Unparsed says the stream continues with text the parser refused.
	Unparsed bool
}

// PastNodesError reports a tree past the node budget. Whole says Nodes counts
// the whole document's tree, as a YAML text is counted once parsed; otherwise
// reading stopped at the first node past the budget, as a JSON text's does.
type PastNodesError struct {
	Limit budget.Limit
	Nodes int
	Whole bool
}

// Error implements error.
func (e *PastNodesError) Error() string {
	if e.Whole {
		return fmt.Sprintf("the document parses to %d nodes, past the %d-node budget", e.Nodes, e.Limit.Cap())
	}
	return fmt.Sprintf("the document holds more than %d nodes, the node budget; reading stopped there", e.Limit.Cap())
}

// errCut reports a walk of a parsed tree cut short with its context live, which
// a walk bounded by no count is not.
var errCut = errors.New("doctree: the walk of the tree was cut short")

// Read reads data: as JSON when it is one JSON text (readJSON,
// docs/parser-library.md#json-as-yaml), else as a YAML stream lexed as YAML 1.2
// and resolved by its core schema (readYAML). A UTF-16 or UTF-32 text is read as
// the UTF-8 it encodes (transcoded), as JSON or YAML alike. A tree
// past maxNodes is a *PastNodesError. Bytes the parser refuses are its error,
// unwrapped, a done ctx's error is ctx's, and either way no tree is returned.
func Read(ctx context.Context, data []byte, maxNodes budget.Limit) (Document, error) {
	if text, ok := transcoded(data); ok {
		data = text
	}
	root, nodes, err := readJSON(ctx, data, maxNodes)
	switch {
	case err == nil:
		return Document{Root: root, Nodes: nodes}, nil
	case !errors.Is(err, errNotJSON):
		return Document{}, stopped(ctx, err)
	default:
		return readYAML(ctx, data, maxNodes)
	}
}

// readYAML reads data as a YAML stream: the first document holding content,
// lexed as YAML 1.2 (respell), what the stream holds past it (readTail), and
// the tree settled, its text restored and its plain scalars resolved by the
// core schema (settle). The tree is counted once parsed and held to maxNodes.
func readYAML(ctx context.Context, data []byte, maxNodes budget.Limit) (Document, error) {
	text, r, dirs := respell(data)
	dec := yaml.NewDecoder(contextReader{ctx: ctx, r: bytes.NewReader(text)})
	root, err := firstWithContent(dec)
	if err != nil {
		return Document{}, stopped(ctx, err)
	}
	rest := readTail(dec)
	// A tail cut short reads as one that did not parse, which it is not.
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}

	doc := Document{Root: root, Rest: rest, Directive: governing(dirs, root)}
	if err := settle(ctx, &doc, r); err != nil {
		return Document{}, err
	}
	if !maxNodes.Admits(doc.Nodes) {
		return Document{}, &PastNodesError{Nodes: doc.Nodes, Limit: maxNodes, Whole: true}
	}
	return doc, nil
}

// governing returns the directive of dirs that governs root: the one written
// from root's line, where the parser places a document opening with directives,
// up to its content's. A directive of another document governs nothing the
// tree holds.
func governing(dirs []Directive, root *yaml.Node) *Directive {
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	for _, d := range dirs {
		if d.Line >= root.Line && d.Line < root.Content[0].Line {
			return &d
		}
	}
	return nil
}
