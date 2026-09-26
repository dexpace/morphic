package load

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// external is what the resolver reads a document through when a reference
// leaves the source and Options.AllowExternalRefs lets it. It stands in for the
// file system and the HTTP client the resolver would otherwise use, reading the
// bytes they would, and prepares the tree those bytes parse to as Load prepares
// the source's: the same budgets and pre-parse refusals, then the anchors
// released (see releaseAnchors).
//
// The prepared tree is stored where the resolver looks for a parsed document
// before it parses the bytes itself, under the key it looks it up by, so the
// model is built from that tree. Without it the resolver parsed an external
// document itself, and the parser skipped every anchored entry the model folds
// into a map there — an operation, a response, a security requirement — with
// no diagnostic (GitHub #501).
//
// Releasing the anchors is only safe behind the refusals. The skip had also
// kept the parser out of a recursive anchor on such an entry, and a folded one
// sends the model build into recursion without end, as a recursive anchor
// anywhere else in an external document already did (GitHub #536): it is
// refused instead.
//
// A file is stored under the path the resolver opens, which is its key. A
// response is stored under its request's URL, which is the resolver's key for
// every URL spelled as net/url spells it. The client sees only the request, so
// an absolute $ref URL spelled otherwise — an upper-case scheme, an empty port —
// misses. resolveExternal detects that miss after the fact and recovers the
// entries it would have cost (GitHub #538).
type external struct {
	doc  *soa.OpenAPI
	opts Options
	// read holds every document prepared, shared by the readers of one compile
	// so a second resolution can be handed what the first read (see resolve).
	read *externalReads
}

// newExternal returns the reader for doc's external references, recording what
// it prepares in read. The document's caches are initialized here rather than
// trusted to be, because storing into an uninitialized one faults inside the
// resolver.
func newExternal(doc *soa.OpenAPI, opts Options, read *externalReads) external {
	doc.InitCache()
	return external{doc: doc, opts: opts, read: read}
}

// Open reads the file name as the resolver's default file system would, and
// prepares it under name.
func (e external) Open(name string) (fs.File, error) {
	f, err := os.Open(name) // the file a $ref names: what AllowExternalRefs opts into
	if err != nil {
		return nil, err
	}
	data, err := e.prepare(name, f)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return preparedFile{File: f, data: bytes.NewReader(data)}, nil
}

// Do fetches req as the resolver's default client would, and prepares the body
// of a successful response under the request's URL. Any other outcome is the
// resolver's to report, as it was.
func (e external) Do(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp, err
	}
	data, err := e.prepare(req.URL.String(), resp.Body)
	if err = errors.Join(err, resp.Body.Close()); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

// prepare reads an external document from r, no further than one byte past
// the byte budget, and stores the tree it parses to under key once the source's
// refusals have passed it. It returns the bytes read, which the resolver still
// reads and caches as it would have.
//
// Bytes that will not parse are handed over unprepared: the resolver parses
// them with the parser this uses and refuses them with its own reason, as it
// does an overlay's.
func (e external) prepare(key string, r io.Reader) ([]byte, error) {
	if limit := e.opts.MaxSourceBytes; limit > 0 {
		r = io.LimitReader(r, int64(limit)+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if exceeds(len(data), e.opts.MaxSourceBytes) {
		return nil, refusedExternal(key, fmt.Sprintf("it is past the %d-byte budget", e.opts.MaxSourceBytes))
	}

	root, parsed := parseTree(data)
	if !parsed {
		return data, nil
	}
	if err := e.admit(key, root); err != nil {
		return nil, err
	}
	releaseAnchors(root)
	e.doc.StoreExternalDocumentInCache(key, root)
	e.read.record(key, data, root)
	return data, nil
}

// parseTree parses data as the resolver parses an external document, and
// reports whether it parsed at all.
func parseTree(data []byte) (*yaml.Node, bool) {
	var root yaml.Node
	return &root, yaml.Unmarshal(data, &root) == nil
}

// admit holds an external document's tree to what Load holds the source's to:
// the pre-parse refusals, then the node budget.
//
// Any finding refuses it, a scan's report of its own fault included, where the
// source carries that fault forward as a warning. A finding about an external
// document has nowhere of its own to be reported — Document.Sources records the
// source alone (GitHub #74) — so the reference that named the document is what
// fails, with the findings as its reason.
func (e external) admit(key string, root *yaml.Node) error {
	findings := refusals(nowhere, root, e.opts)
	if len(findings) > 0 {
		reasons := make([]string, len(findings))
		for i, d := range findings {
			reasons[i] = d.Message
		}
		return refusedExternal(key, strings.Join(reasons, "; "))
	}
	if nodes := nodeCount(root); exceeds(nodes, e.opts.MaxSourceNodes) {
		return refusedExternal(key, fmt.Sprintf("it parses to %d nodes, past the %d-node budget",
			nodes, e.opts.MaxSourceNodes))
	}
	return nil
}

// nowhere locates an external document's findings, which leave as the text of
// an error and never as diagnostics, so where each is does not matter.
func nowhere(*yaml.Node) ir.Provenance {
	return ir.Provenance{Source: ir.NoSource}
}

// refusedExternal is the error a read fails with when the document it read is
// refused, and so the reason the resolver reports for the reference.
func refusedExternal(key, reason string) error {
	return fmt.Errorf("external document %s refused: %s", key, reason)
}

// preparedFile is a file whose bytes were read ahead: Read serves them, and the
// file itself answers Stat and Close.
type preparedFile struct {
	fs.File
	data *bytes.Reader
}

// Read serves the bytes read ahead.
func (p preparedFile) Read(b []byte) (int, error) {
	return p.data.Read(b)
}
