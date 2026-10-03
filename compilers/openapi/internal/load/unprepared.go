package load

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"io"
	"iter"
	"net/http"
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
// read: each prepared document's released tree, by the key it was prepared
// under, the set of those trees, and every answer their requests got, by URL
// and in order. It is safe for concurrent use, as the readers sharing it are.
type externalReads struct {
	mu       sync.Mutex
	trees    map[string]*yaml.Node
	mine     map[*yaml.Node]bool
	answers  map[string][]answer
	replayed map[string]int
}

// newExternalReads returns an empty record.
func newExternalReads() *externalReads {
	return &externalReads{
		trees:    map[string]*yaml.Node{},
		mine:     map[*yaml.Node]bool{},
		answers:  map[string][]answer{},
		replayed: map[string]int{},
	}
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

// recordTree notes a document prepared under key.
func (r *externalReads) recordTree(key string, tree *yaml.Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trees[key] = tree
	r.mine[tree] = true
}

// prepared reports whether tree is one this compile prepared.
func (r *externalReads) prepared(tree *yaml.Node) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mine[tree]
}

// preparedFor returns the tree prepared for the document the resolver keys as
// used, or false for none. A file is keyed by the path the resolver opened,
// which is used itself; a URL by the request built from used, which is what the
// reader was handed.
func (r *externalReads) preparedFor(used string) (*yaml.Node, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tree, ok := r.trees[used]; ok {
		return tree, true
	}
	req, err := http.NewRequest(http.MethodGet, used, nil)
	if err != nil {
		return nil, false
	}
	tree, ok := r.trees[req.URL.String()]
	return tree, ok
}

// handOver stores every prepared tree where the resolver of doc looks for one:
// under the key it was prepared under, and, for each document in used, under
// the key the resolver read it by.
//
// It stores no bytes. With a document's bytes cached the resolver skips its
// request and may return an object cached for another reference, so the
// second resolution would not resolve as the first did (GitHub #576's
// self-reference shows it); the replayed request brings them instead.
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
func resolveExternal(ctx context.Context, at func(jsontext.Pointer) ir.Provenance, doc *soa.OpenAPI, path string,
	opts Options, rebuild func() (*soa.OpenAPI, error),
) (*soa.OpenAPI, []ir.Diagnostic, error) {
	read := newExternalReads()
	reader := newExternal(doc, opts, read)
	diags := resolveWith(ctx, at, doc, path, opts, &reader)
	used := documentsUsed(ctx, doc)
	if !anyAnchored(doc, unprepared(doc, read, used)) {
		return doc, diags, nil
	}

	again, err := rebuild()
	if err != nil {
		return nil, nil, err
	}
	again.InitCache()
	read.handOver(again, used)
	reader = newExternal(again, opts, read)
	reader.replaying = true
	diags = resolveWith(ctx, at, again, path, opts, &reader)
	return again, append(diags, stillUnprepared(at, unprepared(again, read, documentsUsed(ctx, again)))...), nil
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
	switch r := model.(type) {
	case *soa.ReferencedPathItem:
		return hops(r)
	case *soa.ReferencedParameter:
		return hops(r)
	case *soa.ReferencedHeader:
		return hops(r)
	case *soa.ReferencedRequestBody:
		return hops(r)
	case *soa.ReferencedResponse:
		return hops(r)
	case *soa.ReferencedExample:
		return hops(r)
	case *soa.ReferencedLink:
		return hops(r)
	case *soa.ReferencedCallback:
		return hops(r)
	case *soa.ReferencedSecurityScheme:
		return hops(r)
	case *oas3.JSONSchema[oas3.Referenceable]:
		return hops(r)
	default:
		return trail{}
	}
}

// hops follows a reference through each hop its resolution recorded. S is the
// reference's own type, a schema or a Referenced* alias's Reference[T, V, C],
// named through S as resolve.Referenced does because the library's V
// constraint is internal.
//
// A trail that ends on a reference, not an object, ends where the resolution
// stopped. A record holding no object is no hop: the library gives a $ref
// inside a resolved schema one holding only the base it resolves against. A
// schema's GetReferenceChain would not do: it hangs off the target's parent,
// which the last reference to resolve a shared target overwrites.
func hops[S any, R interface {
	*S
	GetReference() references.Reference
	GetReferenceResolutionInfo() *references.ResolveResult[S]
}](ref R) trail {
	var t trail
	var last references.Reference
	hop := ref
	for hop != nil {
		info := hop.GetReferenceResolutionInfo()
		if info == nil || info.Object == nil {
			break
		}
		if len(t.docs) == maxResolutionHops {
			t.cut = true
			return t
		}
		t.docs = append(t.docs, info.AbsoluteDocumentPath)
		_, t.endsInSource = info.ResolvedDocument.(*soa.OpenAPI)
		last = info.AbsoluteReference
		hop = info.Object
	}
	if hop != nil {
		t.stopped = hop.GetReference() // empty for the object a resolution ends on
	}
	if t.stopped == "" {
		t.target = last
	}
	return t
}
