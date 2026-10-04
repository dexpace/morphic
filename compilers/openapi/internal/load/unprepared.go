package load

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json/jsontext"
	"io"
	"iter"
	"net/http"
	"slices"
	"sync"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/ir"
)

// maxResolutionHops bounds how many documents one reference's resolution is
// followed through (styleguide bounded-everything rule). The resolver refuses a
// cycle, so a chain ends on its own; the bound is for an absurd one. A document
// past it is neither recovered nor reported (GitHub #538), nor named as where a
// finding is (findingPlace).
const maxResolutionHops = 32

// externalReads is what the external-document readers of one compile have
// read: each prepared document's released tree and the bytes it came from, by
// the key it was prepared under, its tree by the digest of those bytes, the set
// of those trees, and every answer their requests got, by URL and in order. The
// source is held as a document read already (see sourceDocument). It is safe
// for concurrent use, as the readers sharing it are.
type externalReads struct {
	mu       sync.Mutex
	self     sourceDocument
	trees    map[string]*yaml.Node
	mine     map[*yaml.Node]bool
	answers  map[string][]answer
	replayed map[string]int
	// data holds the bytes each key's tree was prepared from, and byDigest the
	// first tree prepared from bytes of each digest (see recordTree).
	data     map[string][]byte
	byDigest map[digest]*yaml.Node
	// mended holds treeFor's last answer for each path, since the resolver
	// hands the bytes it holds for a document to each reference that hits its
	// cache.
	mended map[string]treeAnswer
	// stood holds the source's reference object each stand-in stands for (see
	// external.holdObject).
	stood map[any]resolvable
}

// digest is the SHA-256 of a document's bytes.
type digest = [sha256.Size]byte

// treeAnswer is the tree treeFor gave for bytes, known by their backing array
// and length.
type treeAnswer struct {
	first *byte
	n     int
	tree  *yaml.Node
}

// newExternalReads returns a record holding only self, under each key the
// resolver looks it up by.
func newExternalReads(self sourceDocument) *externalReads {
	r := &externalReads{
		self:     self,
		trees:    map[string]*yaml.Node{},
		mine:     map[*yaml.Node]bool{},
		answers:  map[string][]answer{},
		replayed: map[string]int{},
		data:     map[string][]byte{},
		byDigest: map[digest]*yaml.Node{},
		mended:   map[string]treeAnswer{},
		stood:    map[any]resolvable{},
	}
	for _, key := range self.keys() {
		r.recordTree(key, self.root, self.data)
	}
	return r
}

// answer is how a request was answered: the error it failed with, or a status
// and the body read for it.
type answer struct {
	status int
	body   []byte
	err    error
}

// response returns a as the client returned it: its error, or a response to req
// with its status and body.
func (a answer) response(req *http.Request) (*http.Response, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(bytes.NewReader(a.body)), Request: req}, nil
}

// recordAnswer appends how a request for key was answered.
func (r *externalReads) recordAnswer(key string, a answer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.answers[key] = append(r.answers[key], a)
}

// nextAnswer returns the first answer for key not yet replayed, so the n-th
// request replayed gets the n-th answer, or false once none is left. The
// resolver sends one request at a time, in its walk's order, so a second
// resolution asks in the order the first did.
func (r *externalReads) nextAnswer(key string) (answer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.replayed[key]
	if n >= len(r.answers[key]) {
		return answer{}, false
	}
	r.replayed[key] = n + 1
	return r.answers[key][n], true
}

// recordTree notes a document prepared under key from data. A digest names the
// first tree prepared from bytes of it: each such tree holds what those bytes
// do, which is all a record that only its bytes identify needs of one, so one
// file prepared under two keys, or once in each resolution of GitHub #538, is
// still found by its bytes. The source is recorded first, so its bytes name its
// tree, overlay and all: another document named only by the same bytes is
// taken for it.
func (r *externalReads) recordTree(key string, tree *yaml.Node, data []byte) {
	sum := sha256.Sum256(data)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trees[key] = tree
	r.mine[tree] = true
	r.data[key] = data
	if _, named := r.byDigest[sum]; !named {
		r.byDigest[sum] = tree
	}
}

// treeFor returns the tree prepared from data, the bytes of the document the
// resolver keys as path, or nil for none (see lookup). Its last answer for each
// path is kept for the same bytes, since the resolver hands the bytes it holds
// for a document to each reference that hits its cache. Only the last is kept:
// the resolver replaces those bytes with a copy each time it reads them for a
// pointer it has not resolved, and keeping each copy's answer would keep each
// copy.
func (r *externalReads) treeFor(path string, data []byte) *yaml.Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(data) == 0 {
		return r.lookup(path, data)
	}
	if last := r.mended[path]; last.first == &data[0] && last.n == len(data) {
		return last.tree
	}
	tree := r.lookup(path, data)
	r.mended[path] = treeAnswer{first: &data[0], n: len(data), tree: tree}
	return tree
}

// mend settles where each of c's records says its hop resolved: the tree
// prepared from a document's bytes in place of the bytes, and the source's
// model, doc, and path in place of the source's bytes or tree, however spelled,
// so a $ref back into the source ends in it as an internal one does. A hop that
// reached a stand-in reached the object it stands for, which a resumed
// resolution resolves as the source does. It reports whether it changed any
// record.
func (r *externalReads) mend(c chain, doc *soa.OpenAPI) bool {
	changed := false
	for _, rec := range c.records {
		if settled := r.settled(*rec.document, rec.path, doc); settled != nil {
			*rec.document = settled
			if settled == any(doc) {
				*rec.documentPath = r.self.path
			}
			changed = true
		}
		if obj, ok := r.stoodFor(rec.object); ok && rec.replace(obj) {
			changed = true
		}
	}
	return changed
}

// standIn returns a stand-in for the source's reference object obj: an empty
// object of its kind, neither a reference nor an inline object, so a hop the
// resolver resolves to it fails. It returns false for a kind with none, a
// schema, which hold holds no stand-in for (see external.holdWalked).
func (r *externalReads) standIn(obj resolvable) (any, bool) {
	stand, ok := emptyOf(obj)
	if !ok {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stood[stand] = obj
	return stand, true
}

// stoodFor returns the source's object stand stands in for, or false when it
// is no stand-in.
func (r *externalReads) stoodFor(stand any) (resolvable, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	obj, ok := r.stood[stand]
	return obj, ok
}

// stoodIn reports whether c stopped at a stand-in: its last hop reached one,
// which a resolution resumed once mend has replaced it gets past.
func (r *externalReads) stoodIn(c chain) bool {
	if len(c.records) == 0 {
		return false
	}
	_, ok := r.stoodFor(c.records[len(c.records)-1].object)
	return ok
}

// sourceKeys returns, sorted, each key the source was recorded under: those it
// is looked up by, and each a reader found a $ref spell it by.
func (r *externalReads) sourceKeys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var keys []string
	for key, tree := range r.trees {
		if tree == r.self.root && r.self.root != nil {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// emptyOf returns an empty object of obj's kind of reference object, or false
// for a kind with no stand-in.
func emptyOf(obj resolvable) (any, bool) {
	switch obj.(type) {
	case *soa.ReferencedPathItem:
		return &soa.ReferencedPathItem{}, true
	case *soa.ReferencedParameter:
		return &soa.ReferencedParameter{}, true
	case *soa.ReferencedHeader:
		return &soa.ReferencedHeader{}, true
	case *soa.ReferencedRequestBody:
		return &soa.ReferencedRequestBody{}, true
	case *soa.ReferencedResponse:
		return &soa.ReferencedResponse{}, true
	case *soa.ReferencedExample:
		return &soa.ReferencedExample{}, true
	case *soa.ReferencedLink:
		return &soa.ReferencedLink{}, true
	case *soa.ReferencedCallback:
		return &soa.ReferencedCallback{}, true
	case *soa.ReferencedSecurityScheme:
		return &soa.ReferencedSecurityScheme{}, true
	default:
		return nil, false
	}
}

// settled returns what a record whose hop read path and resolved against
// document should say instead, or nil when it says it already.
func (r *externalReads) settled(document any, path string, doc *soa.OpenAPI) any {
	var tree *yaml.Node
	switch d := document.(type) {
	case *yaml.Node:
		tree = d
	case []byte:
		tree = r.treeFor(path, d)
	default:
		return nil
	}
	switch {
	case tree == nil:
		return nil
	case tree == r.self.root:
		return doc
	case document == any(tree):
		return nil
	default:
		return tree
	}
}

// lookup is treeFor unmemoized: the tree prepared under the key path names
// (see under) when it was prepared from these very bytes, else the first one
// prepared from bytes of their digest. The bytes are compared, not hashed: a
// record names the key its bytes were prepared under far more often than not.
// The caller holds the lock.
func (r *externalReads) lookup(path string, data []byte) *yaml.Node {
	if key, tree, ok := r.under(path); ok && bytes.Equal(r.data[key], data) {
		return tree
	}
	return r.byDigest[sha256.Sum256(data)]
}

// prepared reports whether tree is one this compile prepared.
func (r *externalReads) prepared(tree *yaml.Node) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mine[tree]
}

// preparedFor returns the tree prepared for the document the resolver keys as
// used, or false for none (see under).
func (r *externalReads) preparedFor(used string) (*yaml.Node, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, tree, ok := r.under(used)
	return tree, ok
}

// under returns the key the document the resolver keys as used was prepared
// under, and its tree, or false for none. A file is keyed by the path the
// resolver opened, which is used itself; a URL by the request built from used,
// which is what the reader was handed. The caller holds the lock.
func (r *externalReads) under(used string) (string, *yaml.Node, bool) {
	if tree, ok := r.trees[used]; ok {
		return used, tree, true
	}
	req, err := http.NewRequest(http.MethodGet, used, nil)
	if err != nil {
		return "", nil, false
	}
	key := req.URL.String()
	tree, ok := r.trees[key]
	return key, tree, ok
}

// handOver stores every prepared tree where the resolver of doc looks for one:
// under the key it was prepared under, and, for each document in used, under
// the key the resolver read it by.
//
// It stores no bytes. With a document's bytes cached the resolver skips its
// request and caches no object it builds, so two references the first
// resolution gave one object would each get their own; the replayed request
// brings them instead. The source's are stored by hold, in both resolutions.
func (r *externalReads) handOver(doc *soa.OpenAPI, used []usedDocument) {
	r.mu.Lock()
	for key, tree := range r.trees {
		doc.StoreExternalDocumentInCache(key, tree)
	}
	r.mu.Unlock()
	for _, u := range used {
		if tree, ok := r.preparedFor(u.key); ok {
			doc.StoreExternalDocumentInCache(u.key, tree)
		}
	}
}

// resolveExternal resolves doc's references through a preparing reader (see
// external), and recovers the entries of a document the resolver parsed itself
// (GitHub #538).
//
// net/url respells a URL many-to-one, so a stored tree can miss the resolver's
// key. Each resolved reference records the document it read; when one the
// resolver parsed holds an anchor, doc is rebuilt and resolved again, handed
// every prepared tree (see handOver) and the first pass's answers (see
// external.replaying). It resolves as the first did but for those trees, which
// the walk never descends into, so it reaches no new document; one found
// unprepared is an internal fault.
func resolveExternal(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, self sourceDocument,
	opts Options, rebuild func() (*soa.OpenAPI, error),
) (*soa.OpenAPI, MappingTargets, []ir.Diagnostic, error) {
	read := newExternalReads(self)
	reader := newExternal(doc, opts, read)
	targets, diags := resolveWith(ctx, at, doc, self, opts, &reader)
	used := documentsUsed(ctx, doc)
	if !anyAnchored(doc, unprepared(doc, read, used)) {
		return doc, targets, diags, nil
	}

	again, err := rebuild()
	if err != nil {
		return nil, MappingTargets{}, nil, err
	}
	again.InitCache()
	read.handOver(again, used)
	reader = newExternal(again, opts, read)
	reader.replaying = true
	targets, diags = resolveWith(ctx, at, again, self, opts, &reader)
	return again, targets, append(diags, stillUnprepared(at, unprepared(again, read, documentsUsed(ctx, again)))...), nil
}

// anyAnchored reports whether any document in missed, as the resolver parsed
// it, carries an anchor — the one thing the fold's skip reads.
func anyAnchored(doc *soa.OpenAPI, missed []usedDocument) bool {
	for _, m := range missed {
		cached, _ := doc.GetCachedExternalDocument(m.key)
		if tree, ok := cached.(*yaml.Node); ok && hasAnchor(tree) {
			return true
		}
	}
	return false
}

// hasAnchor reports whether any node of the tree under root carries an anchor.
// It follows Content and never an alias, so it visits each node once.
func hasAnchor(root *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Anchor != "" {
			return true
		}
		stack = append(stack, n.Content...)
	}
	return false
}

// stillUnprepared reports each document the second resolution found
// unprepared, once, at the first reference that read it.
func stillUnprepared(at func(jsontext.Pointer) ir.Provenance, missed []usedDocument) []ir.Diagnostic {
	var diags []ir.Diagnostic
	reported := map[string]bool{}
	for _, m := range missed {
		if reported[m.key] {
			continue
		}
		reported[m.key] = true
		diags = append(diags, diag.Newf(ir.SeverityError, diag.InternalInvariant, at(m.site),
			"internal: the second resolution read %s itself; "+
				"anchored entries in it may be missing (GitHub #538)", m.key))
	}
	return diags
}

// usedDocument is a document the resolver read for a reference, under the key
// it looked it up by, and the pointer that writes the reference.
type usedDocument struct {
	key  string
	site jsontext.Pointer
}

// documentsUsed returns every document a resolved reference in doc read, at
// every hop of its resolution, in walk order.
func documentsUsed(ctx context.Context, doc *soa.OpenAPI) []usedDocument {
	return hopsUsed(soa.Walk(ctx, doc))
}

// hopsUsed is documentsUsed over walk items rather than a document, so a test
// can reach its early return: Match returns only what its callback returns,
// which here is always nil, so no real walk stops early.
func hopsUsed(items iter.Seq[soa.WalkItem]) []usedDocument {
	var used []usedDocument
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			site := jsontext.Pointer(item.Location.ToJSONPointer())
			for _, key := range resolutionTrail(model).docs {
				used = append(used, usedDocument{key: key, site: site})
			}
			return nil
		}}); err != nil {
			return used
		}
	}
	return used
}

// unprepared returns the documents among used whose tree the resolver parsed
// itself: those whose cached tree is not one read prepared.
func unprepared(doc *soa.OpenAPI, read *externalReads, used []usedDocument) []usedDocument {
	var out []usedDocument
	for _, u := range used {
		cached, ok := doc.GetCachedExternalDocument(u.key)
		tree, isTree := cached.(*yaml.Node)
		if !ok || !isTree || read.prepared(tree) {
			continue
		}
		out = append(out, u)
	}
	return out
}

// trail is what a reference's resolution recorded: the document each hop read,
// first hop first, and either the target it ended on, by absolute reference, or
// the reference it stopped at unresolved. cut marks a trail followed only as far
// as maxResolutionHops. endsInSource marks the last document as the source,
// which a hop resolves against as the model, not as a parsed tree.
type trail struct {
	docs         []string
	target       references.Reference
	stopped      references.Reference
	cut          bool
	endsInSource bool
}

// resolutionTrail returns the trail a reference's resolution recorded, or an
// empty one for a model that is not a reference.
func resolutionTrail(model any) trail {
	return resolutionChain(model).trail()
}

// chain is every record a reference's resolution holds, first hop first, and
// the reference it stopped at, if it stopped at one. The last record holds no
// object when the library gave the reference it stopped at one of its own: a
// $ref inside a resolved schema gets one holding only the base it resolves
// against. cut marks a chain followed only as far as maxResolutionHops.
type chain struct {
	records []record
	stopped references.Reference
	cut     bool
}

// record is one hop's record: the document it read, by path, what it reached,
// the object built there and a walk over it, and where it notes the document
// it resolved against and that document's path, which a later hop resolves
// against in turn. replace puts an object of the hop's kind in place of the
// one it reached, and reports whether obj was of that kind.
type record struct {
	path         string
	target       references.Reference
	object       any
	walk         func(context.Context) iter.Seq[soa.WalkItem]
	document     *any
	documentPath *string
	reached      bool
	replace      func(obj any) bool
}

// trail reads what c's records say about where the resolution went. A record
// holding no object is no hop.
func (c chain) trail() trail {
	t := trail{stopped: c.stopped, cut: c.cut}
	var last references.Reference
	for _, r := range c.records {
		if !r.reached {
			continue
		}
		t.docs = append(t.docs, r.path)
		_, t.endsInSource = (*r.document).(*soa.OpenAPI)
		last = r.target
	}
	if !c.cut && t.stopped == "" {
		t.target = last
	}
	return t
}

// resolutionChain returns the records a reference's resolution holds, or none
// for a model that is not a reference.
func resolutionChain(model any) chain {
	switch r := model.(type) {
	case *soa.ReferencedPathItem:
		return recordsOf(r)
	case *soa.ReferencedParameter:
		return recordsOf(r)
	case *soa.ReferencedHeader:
		return recordsOf(r)
	case *soa.ReferencedRequestBody:
		return recordsOf(r)
	case *soa.ReferencedResponse:
		return recordsOf(r)
	case *soa.ReferencedExample:
		return recordsOf(r)
	case *soa.ReferencedLink:
		return recordsOf(r)
	case *soa.ReferencedCallback:
		return recordsOf(r)
	case *soa.ReferencedSecurityScheme:
		return recordsOf(r)
	case *oas3.JSONSchema[oas3.Referenceable]:
		return recordsOf(r)
	default:
		return chain{}
	}
}

// recordsOf follows a reference through each record its resolution holds. S is
// the reference's own type, a schema or a Referenced* alias's Reference[T, V,
// C], named through S as resolve.Referenced does because the library's V
// constraint is internal.
//
// It walks forward from the reference, each hop owning its record. A schema's
// GetReferenceChain would not do: it hangs off the target's parent, which the
// last reference to resolve a shared target overwrites.
func recordsOf[S any, R interface {
	*S
	GetReference() references.Reference
	GetReferenceResolutionInfo() *references.ResolveResult[S]
}](ref R) chain {
	var c chain
	hop := ref
	for hop != nil {
		info := hop.GetReferenceResolutionInfo()
		if info == nil {
			break
		}
		r := record{path: info.AbsoluteDocumentPath, target: info.AbsoluteReference, document: &info.ResolvedDocument,
			documentPath: &info.AbsoluteDocumentPath, replace: replacing(info)}
		if info.Object == nil {
			c.records = append(c.records, r)
			break
		}
		if len(c.records) == maxResolutionHops {
			c.cut = true
			return c
		}
		r.object, r.walk, r.reached = info.Object, walkOf(info.Object), true
		c.records = append(c.records, r)
		hop = info.Object
	}
	if hop != nil {
		c.stopped = hop.GetReference() // empty for the object a resolution ends on
	}
	return c
}

// replacing returns the replace of info's record: obj becomes the object
// info's hop reached when it is of that hop's kind.
func replacing[S any](info *references.ResolveResult[S]) func(any) bool {
	return func(obj any) bool {
		o, ok := obj.(*S)
		if ok {
			info.Object = o
		}
		return ok
	}
}

// walkOf returns the walk over obj, started at its own kind.
func walkOf[T any](obj *T) func(context.Context) iter.Seq[soa.WalkItem] {
	return func(ctx context.Context) iter.Seq[soa.WalkItem] {
		return soa.Walk(ctx, obj)
	}
}
