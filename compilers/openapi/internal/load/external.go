package load

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/ir"
)

// external is what the resolver reads a document through when
// Options.AllowExternalRefs lets a reference leave the source. It stands in for
// the resolver's file system and HTTP client, and prepares each tree as Load
// does the source's: the same budgets and pre-parse refusals, then
// releaseAnchors.
//
// The tree is stored where the resolver looks for a parsed document, so the
// model builds from it, not from the resolver's own parse, which silently skips
// anchored entries the model folds into a map (GitHub #501). Anchors are
// released only behind the refusals: they reject the recursive anchor that
// would loop the model build (GitHub #536).
type external struct {
	doc  *soa.OpenAPI
	opts Options
	// read holds every document prepared and every request's answer, shared by
	// the readers of one compile so a second resolution can be handed what the
	// first read (see resolveExternal).
	read *externalReads
	// replaying has the reader answer a request from read rather than send it,
	// while read holds one: the n-th request for a URL gets the n-th answer
	// recorded for it. A second resolution's reader replays the first's.
	replaying bool
	// judged holds each document's verdict by the key it was read under: the
	// error it was refused with, or the digest of the bytes it was prepared
	// from. The resolver asks again on every reference until it has built
	// something from a document, so a refused one fails again unread, and a
	// prepared one is judged again only if its bytes changed. Shared by every
	// copy of the reader and safe for concurrent use. A second resolution keeps
	// its own, since a digest vouches for a tree stored in this reader's
	// document; a refusal reaches it as a replayed answer.
	judged *sync.Map
}

// newExternal returns the reader for doc's external references, recording in
// read what it prepares and how its requests are answered. The document's caches
// are initialized here rather than trusted to be, because storing into an
// uninitialized one faults inside the library.
func newExternal(doc *soa.OpenAPI, opts Options, read *externalReads) external {
	doc.InitCache()
	return external{doc: doc, opts: opts, read: read, judged: &sync.Map{}}
}

// hold stores the source where the resolver of e.doc looks for a document it
// has read: its tree and bytes under each key it is looked up by, and each
// object the source's model holds at the pointer naming it. A $ref back into
// the source from another document then reaches the source's own objects, and
// no file is read for it (GitHub #759).
//
// A panic stops the walk where it stands, holding the rest no more than before.
// The resolution walk that follows is the same walk, and reports it.
func (e external) hold(ctx context.Context) {
	e.holdWalked(soa.Walk(ctx, e.doc))
}

// holdWalked is hold over the walk of e.doc the caller supplies, so a test can
// hand it one that panics.
func (e external) holdWalked(items iter.Seq[soa.WalkItem]) {
	keys := e.read.self.keys()
	if len(keys) == 0 {
		return
	}
	for _, key := range keys {
		e.doc.StoreExternalDocumentInCache(key, e.read.self.root)
		e.doc.StoreReferenceDocumentInCache(key, e.read.self.data)
	}
	if _, err := eachModel(items, "source", func(site jsontext.Pointer, r resolvable) error {
		for _, key := range keys {
			e.doc.StoreReferencedObjectInCache(key+"#"+string(site), r)
		}
		return nil
	}); err != nil {
		return
	}
}

// settle finishes resolving r, whose resolution returned vErrs and err. It
// mends the records r's chain holds (see externalReads.mend), and resumes a
// resolution that stalled where mending cures it (see resumable): the library
// reports a document as its bytes when it holds both them and the object a
// reference names, and a hop resolved against bytes fails (GitHub #761). Each
// resumption starts past the hop the last stalled at, so maxResolutionHops
// bounds them. A hop left stalled keeps its record (see chain.withoutStall).
func (e external) settle(ctx context.Context, r resolvable, opts references.ResolveOptions,
	vErrs []error, err error,
) ([]error, error) {
	for range maxResolutionHops {
		c := resolutionChain(r)
		if !e.resumable(r, c, err) {
			break
		}
		e.read.mend(c, e.doc)
		more, again := r.Resolve(ctx, opts)
		vErrs, err = append(vErrs, more...), again
	}
	e.read.mend(resolutionChain(r).withoutStall(), e.doc)
	return vErrs, err
}

// Open reads the file name as the resolver's default file system would, and
// prepares it under name. A file refused once fails again without being opened,
// and the source, spelled as no key hold stored, is served as it is held.
func (e external) Open(name string) (fs.File, error) {
	if e.read.self.names(name) {
		e.doc.StoreExternalDocumentInCache(name, e.read.self.root)
		e.read.recordTree(name, e.read.self.root, e.read.self.data)
		return sourceFile{Reader: bytes.NewReader(e.read.self.data)}, nil
	}
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
// as it was. Every answer is recorded in read, which a replaying reader answers
// from while it can (see replaying).
//
// The resolver looks a document up by its URL as net/url spells it, and the
// client sees only the request, so an absolute $ref spelled otherwise misses;
// resolveExternal recovers that document (GitHub #538).
func (e external) Do(req *http.Request) (*http.Response, error) {
	key := req.URL.String()
	if e.replaying {
		if a, ok := e.read.nextAnswer(key); ok {
			return a.response(req)
		}
	}
	resp, err := e.send(req, key)
	if err != nil {
		e.read.recordAnswer(key, answer{err: err})
		return resp, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		e.read.recordAnswer(key, answer{status: resp.StatusCode})
		return resp, nil
	}
	data, err := e.prepare(key, resp.Body)
	if err = errors.Join(err, resp.Body.Close()); err != nil {
		e.read.recordAnswer(key, answer{err: err})
		return nil, err
	}
	e.read.recordAnswer(key, answer{status: resp.StatusCode, body: data})
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

// send makes req over the network, unless the document under key was refused.
func (e external) send(req *http.Request, key string) (*http.Response, error) {
	if err := e.refusal(key); err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// prepare reads an external document from r, no further than one byte past the
// byte budget, and stores the tree it parses to under key once the source's
// refusals have passed it. It returns the bytes read, which the resolver still
// reads and caches as it would have. Bytes already prepared under key are not
// parsed again: the tree stored then still stands.
//
// Bytes that will not parse are handed over unprepared, and the resolver
// refuses them with its own reason.
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
	e.read.recordTree(key, root, data)
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

// sourceFile serves the source's bytes as the file the resolver opened. The
// resolver reads it and closes it, and asks nothing else of it.
type sourceFile struct {
	*bytes.Reader
}

// Stat refuses: the source is held, not opened.
func (sourceFile) Stat() (fs.FileInfo, error) {
	return nil, fs.ErrInvalid
}

// Close releases nothing: the bytes are the source's own.
func (sourceFile) Close() error {
	return nil
}
