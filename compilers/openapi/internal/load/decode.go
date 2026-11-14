// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package load

import (
	"context"
	"errors"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/budget"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/doctree"
	"github.com/dexpace/morphic/compilers/openapi/internal/provenance"
	"github.com/dexpace/morphic/ir"
)

// Parsed is a source's decoded form, produced once and used twice: detection
// reads the document's keys off it, and the compile it routes to lowers the
// same tree. It travels in compilers.Source.Parsed.
//
// It holds the digest of the bytes it came from and the node budget it was
// read within, so a reader can check it describes the Data beside it under its
// own budget. The digest is what SourceInfo.Hash records.
//
// The tree is live: an overlay patches it in place, so a Parsed belongs to one
// compile.
type Parsed struct {
	doc   doctree.Document
	limit budget.Limit
	hash  string
}

// Root is the document the compile lowers — the first in the stream that holds
// content — for a reader that wants only what the source declares.
func (p *Parsed) Root() *yaml.Node { return p.doc.Root }

// Decode reads source bytes into the document the compile lowers and what
// follows it (doctree.Read), held to maxNodes, for detection to read the same
// document the compile will (GitHub #481) and to leave the parse behind for it
// (Parsed). The error is the reader's own, unwrapped, since detection quotes
// it; the compile wraps it as ErrParse.
func Decode(ctx context.Context, data []byte, maxNodes budget.Limit) (*Parsed, error) {
	doc, err := doctree.Read(ctx, data, maxNodes)
	if err != nil {
		return nil, err
	}
	return &Parsed{hash: ir.SourceHash(data), doc: doc, limit: maxNodes}, nil
}

// parsedFor returns the decoded form of src read within maxNodes: the one its
// caller already made, when it is this compiler's own and describes these very
// bytes under the same budget, and a fresh parse otherwise.
//
// The bytes are compared by content, not by slice identity: a caller reading
// into a pooled buffer hands back the same backing array with different bytes,
// and the compile would lower a tree the source no longer holds while stamping
// SourceInfo.Hash from the bytes it does, breaking the identity that snapshots,
// IR diffing and caching key on (architecture.md §1).
func parsedFor(ctx context.Context, src compilers.Source, maxNodes budget.Limit) (*Parsed, error) {
	// A nil *Parsed stored in the interface is not a nil interface, so the
	// assertion succeeds and hands back nothing to read; ir.IsNilTypeDef and
	// compilers' own nil-compiler screen exist for the same two spellings.
	if p, ok := src.Parsed.(*Parsed); ok && p != nil && p.limit == maxNodes && p.hash == ir.SourceHash(src.Data) {
		return p, nil
	}
	return Decode(ctx, src.Data, maxNodes)
}

// Hash is the lowercase hex SHA-256 of the bytes this parse was built from, for
// SourceInfo.Hash.
func (p *Parsed) Hash() string { return p.hash }

// OverNodeBudget reports whether err is the refusal of a source past the node
// budget, and if so the refusal to report at prov. It is exported for
// detection, which reads a source before Load does and is held to the same
// budget with the same words, as OverByteBudget is.
func OverNodeBudget(prov ir.Provenance, err error) (ir.Diagnostic, bool) {
	return pastNodes(prov, "source", err)
}

// pastNodes reports whether err is a read of the document what names past the
// node budget, and if so its refusal at prov: worded by what the read knows, a
// YAML tree counted whole or a JSON text read up to the first node past.
func pastNodes(prov ir.Provenance, what string, err error) (ir.Diagnostic, bool) {
	var past *doctree.PastNodesError
	if !errors.As(err, &past) {
		return ir.Diagnostic{}, false
	}
	stop := ""
	if !past.Whole {
		stop = "; reading stopped there"
	}
	return diag.Newf(ir.SeverityError, diag.BudgetExceeded, prov, "%s document %s%s", what, pastNodesReason(past), stop), true
}

// pastNodesReason words past as a refusal's reason, the document unnamed.
func pastNodesReason(past *doctree.PastNodesError) string {
	if past.Whole {
		return fmt.Sprintf("parses to %d nodes, past the %d-node budget", past.Nodes, past.Limit.Cap())
	}
	return fmt.Sprintf("holds more than %d nodes, the node budget", past.Limit.Cap())
}

// streamOf is the input a stream was read for, which its finding names.
type streamOf struct {
	// head opens the finding, and dropped says what a document past the one
	// taken was not.
	head, dropped string
}

// The three inputs a stream is read for.
var (
	sourceStream   = streamOf{"only one document of the YAML stream was lowered; ", "the IR does not"}
	overlayStream  = streamOf{"only one document of the overlay's YAML stream was applied; ", "that was not"}
	externalStream = streamOf{"only one document of the YAML stream was read; ", "that was not"}
)

// streamDiagnostics reports what the stream of the input at index, read for
// what, held past the document taken, t, as one error where the first dropped
// document begins, or nothing for a stream of one document (GitHub #1079,
// #1078).
func streamDiagnostics(index int, what streamOf, t doctree.Tail) []ir.Diagnostic {
	if t.Dropped == 0 && !t.Unparsed && !t.Capped {
		return nil
	}
	prov := ir.Provenance{Source: index}
	if t.Line > 0 {
		prov.Position = ir.Position{Line: t.Line, Column: t.Column}
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.StreamDocumentsDropped, prov,
		"%s%s", what.head, describeTail(what, t))}
}

// describeTail spells what the stream held past its first document.
func describeTail(what streamOf, t doctree.Tail) string {
	var parts []string
	switch {
	case t.Capped && t.Dropped > 0:
		parts = append(parts, fmt.Sprintf("at least %d after it hold content %s, and what follows the %d read was not read",
			t.Dropped, what.dropped, t.Dropped))
	case t.Capped:
		parts = append(parts, fmt.Sprintf("the %d after it hold no content, and what follows them was not read",
			doctree.MaxStreamDocuments))
	case t.Dropped == 1:
		parts = append(parts, "the one after it holds content "+what.dropped)
	case t.Dropped > 1:
		parts = append(parts, fmt.Sprintf("the %d after it hold content %s", t.Dropped, what.dropped))
	}
	if t.Unparsed {
		parts = append(parts, "what follows could not be parsed as YAML")
	}
	return strings.Join(parts, ", and ")
}

// versionDiagnostics reports where doc, an input placed by tree, is read as
// YAML 1.2 while declaring another version: a warning at each thing a declared
// YAML 1.1 reads otherwise (doctree.Legacy), and one at the directive declaring
// a later minor, which has no pointer, so its position alone places it.
func versionDiagnostics(tree provenance.Tree, doc doctree.Document) []ir.Diagnostic {
	if doc.Directive == nil {
		return nil
	}
	if doc.Directive.Minor > 2 {
		at := ir.Provenance{Source: tree.Doc.Source, Position: ir.Position{Line: doc.Directive.Line, Column: 1}}
		return []ir.Diagnostic{diag.Newf(ir.SeverityWarning, diag.YAMLVersionReading, at,
			"the document declares YAML 1.%d, a later version than the compiler reads; it is read as YAML 1.2",
			doc.Directive.Minor)}
	}
	diags := make([]ir.Diagnostic, 0, len(doc.Legacy))
	for _, l := range doc.Legacy {
		diags = append(diags, diag.Newf(ir.SeverityWarning, diag.YAMLVersionReading, tree.Node(l.Node),
			"%s", legacyMessage(l)))
	}
	return diags
}

// legacyMessage words l, one thing YAML 1.1 reads otherwise.
func legacyMessage(l doctree.Legacy) string {
	const declared = ", which the compiler reads; YAML 1.1, which the document declares, "
	switch {
	case l.Break == 0:
		return fmt.Sprintf("plain scalar %q is %s in YAML 1.2%sreads %s", l.Node.Value, l.Is, declared, l.Was)
	case l.InComment:
		return fmt.Sprintf("a comment here holds U+%04X, a character in YAML 1.2%sends a line there", l.Break, declared)
	default:
		return fmt.Sprintf("the scalar holds U+%04X, a character in YAML 1.2%sends a line there", l.Break, declared)
	}
}
