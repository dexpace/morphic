package load

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/fragment"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	refscope "github.com/dexpace/morphic/compilers/openapi/internal/resolve"
	"github.com/dexpace/morphic/ir"
)

// external is what the resolver reads a document through when Options.Reader
// lets a reference leave the source. It stands in for the resolver's file
// system and HTTP client, reading every document through that reader, and
// prepares each tree as Load does the source's: the same budgets and pre-parse
// refusals, then releaseAnchors.
//
// The tree is stored where the resolver looks for a parsed document, so the
// model builds from it, not from the resolver's own parse, which drops anchored
// entries (docs/parser-library.md#anchored-entry-dropped, GitHub #501). Anchors
// are released only behind the refusals, which reject a recursive one
// (docs/parser-library.md#alias-cycle, GitHub #536).
type external struct {
	doc  *soa.OpenAPI
	opts Options
	// read holds every document prepared and the answer each document's one
	// read got, shared by the readers of one compile, so a second resolution is
	// answered as the first was and handed what it prepared (see
	// resolveExternal).
	read *externalReads
	// held is the source as hold hands it to the resolver of doc, which Open
	// hands it again under each other spelling the resolver opens it by.
	held *heldSource
	// work is what settle has spent resuming resolutions in doc's pass.
	work *resumeWork
	// ctx bounds the reads Open makes. The resolver's file system takes no
	// context, so the one its resolution runs under is held here, as the
	// resolution holds it.
	ctx context.Context
}

// crossed reports each budget the reads through reader crossed, once every
// read is in: the resolver's and the binding's.
func crossed(at func(jsontext.Pointer) ir.Provenance, reader *external) []ir.Diagnostic {
	return append(reader.work.exhausted(at), reader.read.exhausted(at, reader.opts.MaxExternalDocuments)...)
}

// maxOpenedSpellings is the most spellings Open holds every object of the
// source under. A spelling the scan found holds only the objects its $refs
// name, so they cost no more than the $refs. One Open meets is one the scan
// found no $ref to write, such as a schema's $ref the resolver rebased onto
// its $id, and how many a document presents is its own. Past this, Open holds
// the document alone and the resolver builds a copy of what a $ref names, as
// for any document.
const maxOpenedSpellings = 16

// heldSource is what the resolver of one document is handed as the source:
// each object of a referenced kind its model holds, by the pointer naming it,
// the one key under which a reference object is held as itself, and the keys
// the document is held under, each saying whether every object is held there
// too. Every copy of the reader shares one, and mu guards all but itself,
// which Open adds to while the resolver reads.
type heldSource struct {
	mu     sync.Mutex
	bySite map[jsontext.Pointer]heldObject
	itself string
	keys   map[string]bool
	// opened counts the spellings Open held every object under.
	opened int
}

// heldObject is an object the source's model holds, and the pointer naming it.
type heldObject struct {
	site jsontext.Pointer
	obj  resolvable
}

// newExternal returns the reader for doc's external references, recording in
// read what it prepares and how each document's read was answered. The
// document's caches are initialized here rather than trusted to be, because
// storing into an uninitialized one faults inside the library.
func newExternal(ctx context.Context, doc *soa.OpenAPI, opts Options, read *externalReads) external {
	doc.InitCache()
	return external{doc: doc, opts: opts, read: read,
		held: &heldSource{bySite: map[jsontext.Pointer]heldObject{}, keys: map[string]bool{}}, work: newResumeWork(),
		ctx: ctx}
}

// hold stores the source where the resolver looks for a document it has read:
// under each key it is looked up by with every object, and under each
// spelling a $ref names it by with the objects those $refs name (see holdSite).
// A $ref back into the source from another document reaches the source's own
// objects, and no file is read (GitHub #759).
//
// A reference object is held as itself only under the key an internal $ref
// resolves against, when no $self rebases it: the resolver resolves it there as
// the source does. Elsewhere a stand-in is held (see externalReads.mend).
func (e external) hold(ctx context.Context, sibs Siblings) {
	if len(e.read.self.keys()) == 0 {
		return
	}
	base := e.doc.GetSelf()
	if base == "" {
		e.held.itself = e.read.self.path
		base = e.read.self.path
	}
	e.holdWalked(walkSource(ctx, e.doc, sibs))
	e.holdSpellings(e.read.self.root, base)
}

// holdWalked is hold over the walk of e.doc the caller supplies, so a test can
// hand it one that panics, which stops the walk where it stands, for the
// resolution walk to report. It holds the source under its keys, then each
// spelling recorded (see holdRecorded): a second resolution's. Only reference
// objects are held: the resolver is never asked to resolve a schema.
func (e external) holdWalked(items iter.Seq[soa.WalkItem]) {
	keys := e.read.self.keys()
	if len(keys) == 0 {
		return
	}
	e.held.mu.Lock()
	defer e.held.mu.Unlock()
	// Deferred so a walk that stops early still holds the recorded spellings,
	// and before the unlock.
	defer e.holdRecorded()
	for _, key := range keys {
		e.holdDocument(key, true)
	}
	if _, err := eachModel(items, "source", func(site jsontext.Pointer, r resolvable) error {
		o := heldObject{site: site, obj: r}
		e.held.bySite[site] = o
		for _, key := range keys {
			e.holdObject(key, o)
		}
		return nil
	}); err != nil {
		return
	}
}

// holdRecorded holds the source under each spelling recorded, and under it the
// objects recorded there that the walk collected. The caller holds e.held.mu.
func (e external) holdRecorded() {
	for _, key := range e.read.spelled.keys() {
		e.holdDocument(key, false)
		for _, site := range e.read.spelled.at(key) {
			if o, ok := e.held.bySite[site]; ok {
				e.holdObject(key, o)
			}
		}
	}
}

// holdSite holds the source under key for a $ref spelled by it that names site:
// its document, and the object at site where it has one, once for each pair.
// The resolver asks the cache for exactly the spelling and pointer a $ref
// writes, so these are all it can ask for. A key every object is held under
// holds site already.
func (e external) holdSite(key string, site jsontext.Pointer) {
	e.held.mu.Lock()
	defer e.held.mu.Unlock()
	whole, held := e.held.keys[key]
	if whole {
		return
	}
	if !held {
		e.holdDocument(key, false)
		e.read.spelled.note(key, "")
	}
	if o, ok := e.held.bySite[site]; ok && e.read.spelled.note(key, site) {
		e.holdObject(key, o)
	}
}

// holdOpened holds the source under name, a spelling Open was asked to open: its
// document, and every object while maxOpenedSpellings allows.
func (e external) holdOpened(name string) {
	e.held.mu.Lock()
	defer e.held.mu.Unlock()
	if _, held := e.held.keys[name]; held {
		return
	}
	whole := e.held.opened < maxOpenedSpellings
	e.holdDocument(name, whole)
	e.read.spelled.note(name, "")
	if !whole {
		return
	}
	e.held.opened++
	for site, o := range e.held.bySite {
		if e.read.spelled.note(name, site) {
			e.holdObject(name, o)
		}
	}
}

// holdDocument stores the source's tree under key, and bytes beside it, which
// the resolver needs to find before it reuses a held object. They are empty:
// with a tree held beside them the resolver parses no bytes, but it copies them
// whole whenever it reads the document through them, as it does for every
// schema $ref naming the source's file, so the source's own would cost a copy
// of the source each time. The tree is recorded as read from the bytes held.
// whole says every object is held under key too. The caller holds e.held.mu.
func (e external) holdDocument(key string, whole bool) {
	e.held.keys[key] = whole
	e.doc.StoreExternalDocumentInCache(key, e.read.self.root)
	e.doc.StoreReferenceDocumentInCache(key, []byte{})
	e.read.recordTree(key, e.read.self.root, []byte{})
}

// holdObject stores o under key: itself, or a stand-in for a reference object
// held under any key but e.held.itself.
func (e external) holdObject(key string, o heldObject) {
	held := any(o.obj)
	if key != e.held.itself && o.obj.IsReference() {
		if stand, ok := e.read.standIn(o.obj); ok {
			held = stand
		}
	}
	e.doc.StoreReferencedObjectInCache(key+"#"+string(o.site), held)
}

// holdSpellings holds the source under each spelling a $ref in tree, read
// against base as the resolver reads it, names the source by, before the
// resolver looks one up: for each such $ref, the object it names. Spelled
// otherwise, a back reference is not found held, and the resolver builds a copy
// of what it names (see Open). It visits each node of tree once, following an
// alias only to read a $ref's key and value, as the model does.
func (e external) holdSpellings(tree *yaml.Node, base string) {
	stack := []*yaml.Node{tree}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, n.Content...)
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			if key, value := nodeview.Deref(n.Content[i]), nodeview.Deref(n.Content[i+1]); key.Value == "$ref" && value.Kind == yaml.ScalarNode {
				e.holdSpelling(references.Reference(value.Value), base)
			}
		}
	}
}

// holdSpelling holds the source under the key ref, read against base, names a
// document by, for the pointer ref writes, when that document is the source.
func (e external) holdSpelling(ref references.Reference, base string) {
	if ref.GetURI() == "" {
		return
	}
	abs, err := references.ResolveAbsoluteReference(ref, base)
	if err != nil || !e.read.self.names(abs.AbsoluteReference) {
		return
	}
	e.holdSite(abs.AbsoluteReference, fragment.Pointer(string(ref)))
}

// settle finishes resolving r, whose resolution returned vErrs and err. It
// mends the records r's chain holds (see externalReads.mend), and resumes a
// resolution that stalled where mending cures it (see resumable): the library
// reports a document as its bytes when it holds both them and the object a
// reference names, and a hop resolved against bytes fails (GitHub #761). Each
// resumption starts past the hop the last stalled at, so maxResolutionHops
// bounds them, and is charged to e.work before it reads anything.
func (e external) settle(ctx context.Context, r resolvable, opts references.ResolveOptions,
	vErrs []error, err error,
) ([]error, error) {
	for range maxResolutionHops {
		c := resolutionChain(r)
		resume, refused := e.resumable(c, err)
		if refused != nil {
			err = refused
		}
		if !resume {
			break
		}
		e.read.mend(c, e.doc)
		more, again := r.Resolve(ctx, opts)
		vErrs, err = append(vErrs, more...), again
	}
	e.read.mend(resolutionChain(r), e.doc)
	return vErrs, err
}

// Open serves the file name names as its one read in the compile answered it
// (see externalReads.once), reading it the first time it is asked for (see
// fetch).
//
// The source, spelled as no key holds it, is held under name and served as
// held, not read. That is a spelling no $ref the resolver met was found by
// (see holdSpellings), such as one under a schema's relative $id: the
// resolver builds a copy of what it names, as it does of any document's. Every
// object is held under it, up to maxOpenedSpellings of them.
func (e external) Open(name string) (fs.File, error) {
	if e.read.self.names(name) {
		e.holdOpened(name)
		return servedFile{Reader: bytes.NewReader(nil)}, nil
	}
	a := e.read.once(name, e.opts.MaxExternalDocuments, func() answer { return e.fetch(e.ctx, name, fileNamed(name)) })
	if a.err != nil {
		return nil, a.err
	}
	return servedFile{Reader: bytes.NewReader(a.body)}, nil
}

// fileNamed returns the file the path name names: its dot segments removed, as
// a URI reference's are before anything is read. The resolver cleans a relative
// $ref this way and hands an absolute one over as written, so left to the
// reader, one through a directory that is not there failed, and one through a
// link read another file (GitHub #780). An empty name names no file, where
// cleaned it would name the current directory.
func fileNamed(name string) string {
	if name == "" {
		return name
	}
	return filepath.Clean(name)
}

// Do answers req as the one read in the compile of the document at its URL
// answered it (see externalReads.once), fetching it the first time it is
// asked for (see fetch). A document read is a successful response; a failed
// read is the client's error, which the resolver reports as the reference's
// reason. The resolver hands a file URI here too, for the reader to read as
// the file it names.
//
// The resolver looks a document up by its URL as net/url spells it, so an
// absolute $ref spelled otherwise misses; resolveExternal recovers that
// document (GitHub #538).
func (e external) Do(req *http.Request) (*http.Response, error) {
	key := req.URL.String()
	return e.read.once(key, e.opts.MaxExternalDocuments, func() answer {
		return e.fetch(req.Context(), key, key)
	}).response(req)
}

// fetch reads the document at location through Options.Reader, confined to
// Options.Within and held to the byte budget, and prepares it under key.
func (e external) fetch(ctx context.Context, key, location string) answer {
	if e.opts.Reader == nil {
		return answer{err: refused(key, "no reader was supplied to read it")}
	}
	data, err := e.opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{
		Location: location, Within: e.opts.Within, Limit: e.opts.MaxSourceBytes,
	})
	if err != nil {
		return answer{err: e.readFailure(key, err)}
	}
	data, err = e.prepare(key, data)
	if err != nil {
		return answer{err: err}
	}
	return answer{body: data}
}

// readFailure returns the error a reference into the document under key fails
// with when the reader refused it: a document past the byte budget, outside
// Options.Within or on the network a confined read never reaches is refused
// with the reason, and any other failure is the reader's own account of it,
// passed through.
func (e external) readFailure(key string, err error) error {
	switch {
	case errors.Is(err, compilers.ErrDocumentTooLarge):
		return refused(key, fmt.Sprintf("it is past the %d-byte budget", e.opts.MaxSourceBytes))
	case errors.Is(err, compilers.ErrOutsideWithin) && overNetwork(key):
		return refused(key, "it is on the network, which a read reaches only when external references are allowed")
	case errors.Is(err, compilers.ErrOutsideWithin):
		return refused(key, fmt.Sprintf("it lies outside the source's directory %q, "+
			"which a read leaves only when external references are allowed", e.opts.Within))
	default:
		return err
	}
}

// prepare stores the tree an external document's bytes parse to under key, once
// the source's refusals have passed it, its $refs rewritten as the source's are
// (see escapes), and returns the bytes, which the resolver still reads and
// caches. It runs once per document a compile reads (see externalReads.once).
//
// The byte budget is checked again, though the reader held the read to it:
// admitting bytes past it would let a reader lift a budget the compile set.
// Bytes that will not parse are handed over unprepared, for the resolver to
// refuse with its own reason.
func (e external) prepare(key string, data []byte) ([]byte, error) {
	if exceeds(len(data), e.opts.MaxSourceBytes) {
		return nil, refused(key, fmt.Sprintf("it is past the %d-byte budget", e.opts.MaxSourceBytes))
	}

	root, parsed := parseTree(data)
	if !parsed {
		return data, nil
	}
	if err := e.admit(key, root); err != nil {
		return nil, err
	}
	releaseAnchors(root)
	e.read.self.escapes.escape(root)
	e.doc.StoreExternalDocumentInCache(key, root)
	e.read.recordTree(key, root, data)
	e.holdSpellings(root, key)
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
// where the source carries that one forward as a warning. A refused document
// is not read into the compile, so it has no Document.Sources entry a finding
// could name: the reference that named the document is what fails, with the
// findings as its reason.
func (e external) admit(key string, root *yaml.Node) error {
	findings := refusals(nowhere, root, e.opts)
	if len(findings) > 0 {
		reasons := make([]string, len(findings))
		for i, d := range findings {
			reasons[i] = d.Message
		}
		return refused(key, strings.Join(reasons, "; "))
	}
	if nodes := nodeCount(root); exceeds(nodes, e.opts.MaxSourceNodes) {
		return refused(key, fmt.Sprintf("it parses to %d nodes, past the %d-node budget",
			nodes, e.opts.MaxSourceNodes))
	}
	if reason, refuse := reachesLocal(root, key); refuse {
		return refused(key, "it is fetched over the network and "+reason+
			", which a remote document must not do")
	}
	return nil
}

// reachesLocal returns why root, a document fetched over the network, must not
// be read, when a $ref or $id in it takes the resolver to the local file
// system, and whether one does (GitHub #929). base keys the document.
//
// A $ref naming a local file against base is one (localTarget). So is a $id
// naming a local location, which would rebase a relative $ref under it onto
// the disk; refusing it keeps every base off the disk without following the
// resolver's $id scopes. A urn: $id names no file. The walk reads a value
// through an alias.
func reachesLocal(root *yaml.Node, base string) (reason string, refuse bool) {
	if !overNetwork(base) {
		return "", false
	}
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, n.Content...)
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			key, value := nodeview.Deref(n.Content[i]), nodeview.Deref(n.Content[i+1])
			if value.Kind != yaml.ScalarNode || (key.Value != "$ref" && key.Value != "$id") {
				continue
			}
			t, local := localTarget(value.Value, base)
			if local && key.Value == "$ref" {
				return fmt.Sprintf("its $ref %q reaches the local file %s", value.Value, t), true
			}
			if local {
				return fmt.Sprintf("its $id %q rebases references onto the local %s", value.Value, t), true
			}
		}
	}
	return "", false
}

// overNetwork reports whether key names a document the reader fetched over the
// network: an http or https URL. A file: URI is local, and so is a path.
func overNetwork(key string) bool {
	u, err := url.Parse(key)
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

// localTarget returns the location the $ref or $id value ref, read against
// base, names, and whether that is local rather than a URL — a file: URI among
// them, which the resolver reads from disk (readFileURI). A value the resolver
// cannot place names nothing, and is left for it to report.
func localTarget(ref, base string) (string, bool) {
	abs, err := references.ResolveAbsoluteReference(references.Reference(ref), base)
	if err != nil {
		return "", false
	}
	target := abs.AbsoluteReference
	return target, !refscope.IsURL(target) || isFileURI(target)
}

// isFileURI reports whether s is a file: URI.
func isFileURI(s string) bool {
	u, err := url.Parse(s)
	return err == nil && strings.EqualFold(u.Scheme, "file")
}

// nowhere locates an external document's findings, which leave as the text of
// an error and never as diagnostics, so where each is does not matter.
func nowhere(*yaml.Node) ir.Provenance {
	return ir.Provenance{Source: ir.NoSource}
}

// refused returns the error the read of the document under key fails with,
// refused for reason, which the resolver reports as the reason each reference
// naming it could not be resolved.
func refused(key, reason string) error {
	return fmt.Errorf("external document %s refused: %s", key, reason)
}

// servedFile serves bytes read already as the file the resolver opened: a
// document's, from its one read, or those held for the source (see
// holdDocument). The resolver reads it and closes it, and asks nothing else of
// it.
type servedFile struct {
	*bytes.Reader
}

// Stat refuses: what is served was read, and no file is open.
func (servedFile) Stat() (fs.FileInfo, error) {
	return nil, fs.ErrInvalid
}

// Close releases nothing: there is nothing opened.
func (servedFile) Close() error {
	return nil
}
