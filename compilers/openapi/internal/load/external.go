package load

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"

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
// misses, and the resolver parses that document itself (GitHub #538).
//
// Each verdict is kept for the compile. The resolver keeps a document only
// once it has built something from it, and asks for it again on every reference
// until then: every reference into a refused one, and every one into a document
// whose earlier references built nothing. A refused document fails again
// unread: reading, parsing and scanning it per reference would multiply the
// cost its budgets bound. A prepared one is read again, as the default reader
// would read it, and judged again only if its bytes changed: while they are the
// same its tree still stands where the resolver looks, but the resolver parses
// what it reads itself wherever it misses that tree (GitHub #538), so changed
// bytes must not reach it unjudged.
type external struct {
	doc  *soa.OpenAPI
	opts Options
	// judged holds each document's verdict by the key it was read under: the
	// error it was refused with, or the digest of the bytes it was prepared
	// from. It is shared by every copy of the reader, and safe for concurrent
	// use, which nothing in the resolver's interfaces rules out.
	judged *sync.Map
}

// newExternal returns the reader for doc's external references. The document's
// caches are initialized here rather than trusted to be, because storing into
// an uninitialized one faults inside the library.
func newExternal(doc *soa.OpenAPI, opts Options) external {
	doc.InitCache()
	return external{doc: doc, opts: opts, judged: &sync.Map{}}
}

// Open reads the file name as the resolver's default file system would, and
// prepares it under name. A file refused once fails again without being opened.
func (e external) Open(name string) (fs.File, error) {
	if err := e.refusal(name); err != nil {
		return nil, err
	}
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
// of a successful response under the request's URL. A URL refused once fails
// again without being fetched. Any other outcome is the resolver's to report,
// as it was.
func (e external) Do(req *http.Request) (*http.Response, error) {
	key := req.URL.String()
	if err := e.refusal(key); err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp, err
	}
	data, err := e.prepare(key, resp.Body)
	if err = errors.Join(err, resp.Body.Close()); err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

// prepare reads an external document from r, no further than one byte past
// the byte budget, and stores the tree it parses to under key once the source's
// refusals have passed it. It returns the bytes read, which the resolver still
// reads and caches as it would have. The bytes a document under key was
// prepared from before are not parsed again: the tree the resolver looks up is
// the one stored then.
//
// Bytes that will not parse are handed over unprepared: the resolver parses
// them with the parser this uses and refuses them with its own reason, as it
// does an overlay's.
func (e external) prepare(key string, r io.Reader) ([]byte, error) {
	// One byte past the budget tells a document over it from one at it. At
	// math.MaxInt no document can pass the budget, and that byte would overflow.
	if limit := e.opts.MaxSourceBytes; limit > 0 && limit < math.MaxInt {
		r = io.LimitReader(r, int64(limit)+1)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if exceeds(len(data), e.opts.MaxSourceBytes) {
		return nil, e.refuse(key, fmt.Sprintf("it is past the %d-byte budget", e.opts.MaxSourceBytes))
	}
	if e.prepared(key, data) {
		return data, nil
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
	e.judged.Store(key, sha256.Sum256(data))
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
// Any finding refuses it, the scan's report that it could not finish included,
// where the source carries that one forward as a warning. A finding about an
// external document has nowhere of its own to be reported — Document.Sources
// records the source alone (GitHub #74) — so the reference that named the
// document is what fails, with the findings as its reason.
func (e external) admit(key string, root *yaml.Node) error {
	findings := refusals(nowhere, root, e.opts)
	if len(findings) > 0 {
		reasons := make([]string, len(findings))
		for i, d := range findings {
			reasons[i] = d.Message
		}
		return e.refuse(key, strings.Join(reasons, "; "))
	}
	if nodes := nodeCount(root); exceeds(nodes, e.opts.MaxSourceNodes) {
		return e.refuse(key, fmt.Sprintf("it parses to %d nodes, past the %d-node budget",
			nodes, e.opts.MaxSourceNodes))
	}
	return nil
}

// nowhere locates an external document's findings, which leave as the text of
// an error and never as diagnostics, so where each is does not matter.
func nowhere(*yaml.Node) ir.Provenance {
	return ir.Provenance{Source: ir.NoSource}
}

// refuse records the document under key as refused for reason, and returns the
// error its read fails with, which the resolver reports as the reason the
// reference naming it could not be resolved.
func (e external) refuse(key, reason string) error {
	err := fmt.Errorf("external document %s refused: %s", key, reason)
	e.judged.Store(key, err)
	return err
}

// refusal returns the error the document under key was refused with, or nil
// for one not refused.
func (e external) refusal(key string) error {
	verdict, _ := e.judged.Load(key)
	err, _ := verdict.(error)
	return err
}

// prepared reports whether the document under key was prepared from these very
// bytes already.
func (e external) prepared(key string, data []byte) bool {
	verdict, _ := e.judged.Load(key)
	digest, ok := verdict.([sha256.Size]byte)
	return ok && digest == sha256.Sum256(data)
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
