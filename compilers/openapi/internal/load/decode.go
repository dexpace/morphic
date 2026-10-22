package load

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// decodeStream parses source bytes into the node tree of the document the
// compile lowers, the first with content, and reads what follows so extra
// documents are reported, not dropped. The error is the parser's own, which
// detection quotes and Load wraps as ErrParse.
//
// It is split from unmarshal so an overlay can patch the tree in between;
// re-parsing after serialising would renumber every line. It bounds no alias
// expansion, because a node tree holds an alias as one node, and scan's weigher
// refuses a billion-laughs document. It needs no recover: yaml.v3 turns its own
// faults into errors.
func decodeStream(data []byte) (*yaml.Node, tail, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	root, err := firstWithContent(dec)
	if err != nil {
		return nil, tail{}, err
	}
	return root, readTail(dec), nil
}

// Parsed is a source's decoded form, produced once and used twice: detection
// reads the document's keys off it, and the compile it routes to lowers the
// same tree. It travels in compilers.Source.Parsed.
//
// It holds the digest of the bytes it came from, so a reader can check that it
// describes the Data beside it and read the bytes afresh if not. The digest is
// what SourceInfo.Hash records, so a document's hash is that of the bytes it
// was lowered from.
//
// The tree is live: an overlay patches it in place, so a Parsed belongs to one
// compile.
type Parsed struct {
	hash string
	root *yaml.Node
	rest tail
}

// Root is the document the compile lowers — the first in the stream that holds
// content — for a reader that wants only what the source declares.
func (p *Parsed) Root() *yaml.Node { return p.root }

// Decode parses source bytes into the document the compile lowers and what
// follows it. It is decodeStream under an exported name, for detection to read
// the same document the compile will (GitHub #481) and to leave the parse
// behind for it (Parsed). The error is the parser's own, unwrapped, since
// detection quotes it; the compile wraps it as ErrParse.
func Decode(data []byte) (*Parsed, error) {
	root, rest, err := decodeStream(data)
	if err != nil {
		return nil, err
	}
	return &Parsed{hash: ir.SourceHash(data), root: root, rest: rest}, nil
}

// parsedFor returns the decoded form of src: the one its caller already made,
// when it is this compiler's own and describes these very bytes, and a fresh
// parse otherwise.
//
// The bytes are compared by content, not by slice identity: a caller reading
// into a pooled buffer hands back the same backing array with different bytes,
// and the compile would lower a tree the source no longer holds while stamping
// SourceInfo.Hash from the bytes it does, breaking the identity that snapshots,
// IR diffing and caching key on (architecture.md §1). The digest costs nothing
// extra: SourceInfo.Hash records it anyway.
func parsedFor(src compilers.Source) (*Parsed, error) {
	// A nil *Parsed stored in the interface is not a nil interface, so the
	// assertion succeeds and hands back nothing to read; ir.IsNilTypeDef and
	// compilers' own nil-compiler screen exist for the same two spellings.
	if p, ok := src.Parsed.(*Parsed); ok && p != nil && p.hash == ir.SourceHash(src.Data) {
		return p, nil
	}
	return Decode(src.Data)
}

// Hash is the lowercase hex SHA-256 of the bytes this parse was built from, for
// SourceInfo.Hash.
func (p *Parsed) Hash() string { return p.hash }

// firstWithContent reads documents from dec until one holds content, and
// returns it. A leading document that decodes to null, such as a bare `---`, is
// what a file assembled from fragments begins with; taking it as the document
// would refuse the spec behind it.
//
// A stream with no such document yields the first one it holds, a null the
// model build refuses, or a node of no kind when it holds none; the two reach
// different refusals. A decoder error before content is the source's. The
// search reads at most maxStreamDocuments documents; past it the first one read
// stands.
func firstWithContent(dec *yaml.Decoder) (*yaml.Node, error) {
	var first *yaml.Node
	for range maxStreamDocuments {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if holdsContent(&doc) {
			return &doc, nil
		}
		if first == nil {
			first = &doc
		}
	}
	if first == nil {
		return &yaml.Node{}, nil
	}
	return first, nil
}

// maxStreamDocuments bounds how many documents decodeStream reads past the one
// it takes, to count them, and how many it reads to find one. Each is parsed
// into a tree that is never lowered, so the bound caps what a stream can cost
// beyond the byte budget it already fits; a stream past it is reported as
// holding at least that many, never as fewer.
const maxStreamDocuments = 1024

// tail is what a source carries past the document the compiler lowers. Its
// zero value is the answer for a source with one document holding content:
// nothing dropped.
type tail struct {
	// line and column are where the first dropped document that holds content
	// begins, for the diagnostic to name; zero when none parsed. yaml.v3 counts
	// lines from one, so zero is not a position. Only the position is kept: the
	// document's tree is never lowered and is left to be collected.
	line, column int
	// dropped counts the documents past the first that hold content, up to
	// maxStreamDocuments.
	dropped int
	// capped reports that counting stopped at maxStreamDocuments, so dropped
	// is a floor.
	capped bool
	// unparsed reports that the stream continued with content yaml.v3 could
	// not parse. What it could not parse is not always malformed: a later
	// document opening with a %YAML directive is refused as "incompatible", as
	// it would be as the first document too.
	unparsed bool
}

// readTail reads the documents that follow the one decodeStream took, counting
// those that hold content. A document that holds nothing — a bare separator,
// an explicit null — is what a trailing `---` produces, and skipping it drops
// nothing, on the same reading firstWithContent skips a leading one by.
func readTail(dec *yaml.Decoder) tail {
	var t tail
	for range maxStreamDocuments {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return t
		}
		if err != nil {
			t.unparsed = true
			return t
		}
		if holdsContent(&doc) {
			if t.line == 0 {
				t.line, t.column = doc.Content[0].Line, doc.Content[0].Column
			}
			t.dropped++
		}
	}
	t.capped = true
	return t
}

// holdsContent reports whether a decoded document wraps a root that is not the
// null scalar an empty document decodes to. yaml.v3 wraps exactly one root in
// every document it parses; the length check is the guard, not a case.
func holdsContent(doc *yaml.Node) bool {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return false
	}
	root := doc.Content[0]
	return root.Kind != yaml.ScalarNode || root.ShortTag() != "!!null"
}

// diagnostics reports the drop as one error diagnostic, anchored where the
// first dropped document begins, or nothing for a single-document source.
func (t tail) diagnostics(srcIndex int) []ir.Diagnostic {
	if t.dropped == 0 && !t.unparsed {
		return nil
	}
	prov := ir.Provenance{Source: srcIndex}
	if t.line > 0 {
		prov.Position = ir.Position{Line: t.line, Column: t.column}
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.StreamDocumentsDropped, prov,
		"only one document of the YAML stream was lowered; %s", t.describe())}
}

// describe spells what the stream held past its first document.
func (t tail) describe() string {
	var parts []string
	switch {
	case t.capped:
		parts = append(parts, fmt.Sprintf("at least %d after it hold content the IR does not", t.dropped))
	case t.dropped == 1:
		parts = append(parts, "the one after it holds content the IR does not")
	case t.dropped > 1:
		parts = append(parts, fmt.Sprintf("the %d after it hold content the IR does not", t.dropped))
	}
	if t.unparsed {
		parts = append(parts, "what follows could not be parsed as YAML")
	}
	return strings.Join(parts, ", and ")
}
