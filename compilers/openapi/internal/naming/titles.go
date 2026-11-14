// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package naming

import (
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// reading is what a Namer reads of the way to one position: how the grammar
// reads each position on it (bind.Table.Kinds) and the node written at each,
// the root's first, so kinds[k] and path[k] are what tokens[:k] leads to. path
// ends early where the document holds no node further on.
type reading struct {
	kinds []bind.Kind
	path  []*yaml.Node
}

// read returns the reading of the way to p, empty for a Namer with no table.
func (n *Namer) read(p bind.Position) reading {
	if n.table == nil {
		return reading{}
	}
	r := reading{kinds: n.table.Kinds(p)}
	if p.Doc >= 0 && int(p.Doc) < n.table.Documents() {
		r.path, _ = n.view(p.Doc).DocumentPath(n.table.Document(p.Doc).Root, p.Pointer)
	}
	return r
}

// Title returns the name the title of the schema at p gives the node built
// there (titles.at), and false where it gives none, as walking the structure
// to p reads it.
func (n *Namer) Title(p bind.Position) (string, bool) {
	hint, _, titled := n.walk(p)
	if !titled {
		return "", false
	}
	return hint, true
}

// titles reads the title a schema names its node by. It remembers the words
// of each schema node's title (words), those each oneOf or anyOf list's
// branches write (listed), and the names of the component schemas
// (componentNames), each read once, so a title is read once however many
// positions beneath it are named: like a name, none reads more than the
// documents.
type titles struct {
	table      *bind.Table
	read       map[*yaml.Node]string
	lists      map[bind.Position]map[string]int
	components map[string]bool
}

// at returns the words of the title the schema tokens leads to in doc writes,
// as r reads the way there, where they name the node built there; union marks
// a oneOf or anyOf branch. They name none where the grammar reads no schema
// there or the document holds no node; at a component schema, which its key
// names; where the title has no word; and where its words are a component
// schema's name, or another branch's of the list a branch is in (collides).
// The position then keeps the name it has without a title.
func (t *titles) at(doc bind.DocID, tokens []string, r reading, union bool) string {
	k := len(tokens)
	if k >= len(r.path) || k >= len(r.kinds) || r.kinds[k] != bind.KindSchema || componentRoot(tokens) {
		return ""
	}
	words := t.words(r.path[k])
	if words == "" || t.componentNames()[words] {
		return ""
	}
	if union && t.collides(doc, tokens, r.path[k-1], words) {
		return ""
	}
	return words
}

// componentRoot reports whether tokens leads to a component schema, which its
// key names however its title reads (ids.ComponentSchema).
func componentRoot(tokens []string) bool {
	return len(tokens) == componentSchemaTokens && tokens[0] == "components" && tokens[1] == "schemas"
}

// collides reports whether words, the title of the branch tokens leads to in
// doc, of the list written as list, would name it as another branch of the
// list is named: a title two branches write names neither, whichever comes
// first, and one spelling a branch's positional name (positionalBranch) is
// that branch's, its own included, which the title would name as its position
// does.
func (t *titles) collides(doc bind.DocID, tokens []string, list *yaml.Node, words string) bool {
	if t.listed(doc, tokens[:len(tokens)-1], list)[words] > 1 {
		return true
	}
	index, positional := strings.CutPrefix(words, positionalBranch(""))
	j, err := strconv.Atoi(index)
	return positional && err == nil && strconv.Itoa(j) == index && j < len(items(list))
}

// listed returns how many branches of the list written as list at holder in
// doc write a title of each set of words, read once per list.
func (t *titles) listed(doc bind.DocID, holder []string, list *yaml.Node) map[string]int {
	at := bind.Position{Doc: doc, Pointer: pointerOf(holder)}
	if counted, ok := t.lists[at]; ok {
		return counted
	}
	counted := map[string]int{}
	for _, item := range items(list) {
		if words := t.words(nodeview.Deref(item)); words != "" {
			counted[words]++
		}
	}
	t.lists[at] = counted
	return counted
}

// componentNames returns the canonical name of each component schema a
// document the table holds declares, read once: the names a title gives no
// node, since a declared type is named so.
func (t *titles) componentNames() map[string]bool {
	if t.components != nil {
		return t.components
	}
	t.components = map[string]bool{}
	for id := range t.table.Documents() {
		root := t.table.Document(bind.DocID(id)).Root
		for _, pair := range nodeview.Pairs(nodeview.Child(nodeview.Child(root, "components"), "schemas")) {
			if words := ir.CanonicalWords(pair.Key); words != "" {
				t.components[words] = true
			}
		}
	}
	return t.components
}

// items returns the items of the sequence n stands for, none where it is no
// sequence: a oneOf or an allOf written as a mapping, which load reports, has
// no branches to read.
func items(n *yaml.Node) []*yaml.Node {
	n = nodeview.Deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

// words returns titleWords of node, read once per node.
func (t *titles) words(node *yaml.Node) string {
	if words, ok := t.read[node]; ok {
		return words
	}
	words := titleWords(node)
	t.read[node] = words
	return words
}

// titleWords returns the words of the title the schema written as node holds:
// its own, else the one the inline branches of its allOf write between them,
// since a conjunct describes the instance the schema does. It is "" where
// neither writes one with a word, and where those branches write two; a
// branch holding a $ref is named by it, and one nested in a branch is not
// read.
func titleWords(node *yaml.Node) string {
	if words := ownTitle(node); words != "" {
		return words
	}
	found := ""
	for _, branch := range items(nodeview.Child(node, "allOf")) {
		branch = nodeview.Deref(branch)
		words := ownTitle(branch)
		switch {
		case words == "" || nodeview.Child(branch, "$ref") != nil:
		case found != "" && words != found:
			return ""
		default:
			found = words
		}
	}
	return found
}

// ownTitle returns the words of the title the schema written as node writes
// itself, read as the parser reads a string field (scalarText).
func ownTitle(node *yaml.Node) string {
	return ir.CanonicalWords(scalarText(nodeview.Child(node, "title")))
}
