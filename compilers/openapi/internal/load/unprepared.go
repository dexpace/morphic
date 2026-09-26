package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"net/http"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/references"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/ir"
)

// maxResolutionHops bounds how many documents one reference's resolution is
// followed through (styleguide bounded-everything rule). The resolver refuses a
// cycle, so a chain ends on its own; the bound is for an absurd one.
const maxResolutionHops = 32

// externalReads is what the external-document readers of one compile have
// prepared: each document's bytes and released tree, by the key each was
// prepared under, and the set of those trees.
type externalReads struct {
	data  map[string][]byte
	trees map[string]*yaml.Node
	mine  map[*yaml.Node]bool
}

// newExternalReads returns an empty record.
func newExternalReads() *externalReads {
	return &externalReads{
		data:  map[string][]byte{},
		trees: map[string]*yaml.Node{},
		mine:  map[*yaml.Node]bool{},
	}
}

// record notes a document prepared under key.
func (r *externalReads) record(key string, data []byte, tree *yaml.Node) {
	r.data[key] = data
	r.trees[key] = tree
	r.mine[tree] = true
}

// preparedFor returns what was prepared for the document the resolver keys as
// used, or false for none. A file is keyed by the path the resolver opened,
// which is used itself; a URL by the request built from used, which is what the
// reader was handed.
func (r *externalReads) preparedFor(used string) ([]byte, *yaml.Node, bool) {
	if tree, ok := r.trees[used]; ok {
		return r.data[used], tree, true
	}
	req, err := http.NewRequest(http.MethodGet, used, nil)
	if err != nil {
		return nil, nil, false
	}
	key := req.URL.String()
	tree, ok := r.trees[key]
	return r.data[key], tree, ok
}

// resolveExternal resolves doc's references with external documents read
// through a reader that prepares them (see external), and recovers the entries
// of any document the resolver parsed itself instead (GitHub #538).
//
// The reader stores each prepared tree under the key the request it is handed
// spells, and the resolver looks a document up under the reference's own
// absolute URL. net/url respells a URL many-to-one — an upper-case scheme, an
// empty port — so for a $ref written that way the two keys differ: the resolver
// found nothing, parsed the bytes itself, and skipped every anchored entry the
// model folds into a map, with no diagnostic.
//
// The key the resolver used cannot be recovered from the request, but it can be
// read back afterwards: each resolved reference records the document it read,
// and the tree the resolver cached under that key is either one this compile
// prepared or one it parsed itself. When one it parsed itself holds an anchor —
// without one there is nothing its fold could skip — doc is rebuilt from the
// same tree and resolved again, handed every document the first resolution read
// under the exact key the resolver used. Nothing is read twice, and every
// lookup finds a prepared tree.
//
// The second resolution reads no document the first did not. What the fold
// skipped is an entry of a resolved object, and the walk that resolves the
// references never descends into a resolved object, so recovering one reaches
// no reference the first resolution left unfollowed. A document it still finds
// unprepared would break that, and is reported as the compiler's own fault
// rather than dropped in silence.
func resolveExternal(ctx context.Context, locate scan.Locator, doc *soa.OpenAPI, path string, opts Options,
	rebuild func() (*soa.OpenAPI, error),
) (*soa.OpenAPI, []ir.Diagnostic, error) {
	read := newExternalReads()
	reader := newExternal(doc, opts, read)
	diags := resolveWith(ctx, locate, doc, path, opts, &reader)
	used := documentsUsed(ctx, doc)
	if !anyAnchored(doc, unprepared(doc, read, used)) {
		return doc, diags, nil
	}

	again, err := rebuild()
	if err != nil {
		return nil, nil, err
	}
	again.InitCache()
	for _, u := range used {
		if data, tree, ok := read.preparedFor(u.key); ok {
			again.StoreExternalDocumentInCache(u.key, tree)
			again.StoreReferenceDocumentInCache(u.key, data)
		}
	}
	reader = newExternal(again, opts, read)
	diags = resolveWith(ctx, locate, again, path, opts, &reader)
	return again, append(diags, stillUnprepared(locate, unprepared(again, read, documentsUsed(ctx, again)))...), nil
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
// unprepared, once, naming the first reference that read it.
func stillUnprepared(locate scan.Locator, missed []usedDocument) []ir.Diagnostic {
	var diags []ir.Diagnostic
	reported := map[string]bool{}
	for _, m := range missed {
		if reported[m.key] {
			continue
		}
		reported[m.key] = true
		diags = append(diags, diag.Newf(ir.SeverityError, diag.InternalInvariant, locate(nil),
			"internal: the resolver read %s itself for the $ref at %s after it was handed a prepared copy; "+
				"anchored entries in it may be missing (GitHub #538)", m.key, m.site))
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

// hopsUsed is documentsUsed's walk, over items rather than a document directly,
// so a test can fabricate a WalkItem whose Match reports an error —
// matchSchemas (load.go) takes the same iter.Seq[soa.WalkItem] parameter for
// the same reason, over the same library. Any's own callback below always
// returns nil, so nothing through the real soa.Walk can reach that branch; a
// fabricated item is the only way to.
func hopsUsed(items iter.Seq[soa.WalkItem]) []usedDocument {
	var used []usedDocument
	for item := range items {
		if err := item.Match(soa.Matcher{Any: func(model any) error {
			site := jsontext.Pointer(item.Location.ToJSONPointer())
			for _, key := range hopDocuments(model) {
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
		if !ok || !isTree || read.mine[tree] {
			continue
		}
		out = append(out, u)
	}
	return out
}

// hopDocuments returns the document each hop of a resolved reference's
// resolution read, first hop first, or nothing for a model that is not one.
func hopDocuments(model any) []string {
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
		return schemaHops(r)
	default:
		return nil
	}
}

// hops follows a resolved reference through each document its resolution read.
// Every Referenced* alias is the library's Reference[T, V, C]; S names it the
// way resolve.Referenced does, since the library's V constraint is internal.
func hops[S any, R interface {
	*S
	GetReferenceResolutionInfo() *references.ResolveResult[S]
}](ref R) []string {
	var docs []string
	for hop := ref; hop != nil && len(docs) < maxResolutionHops; {
		info := hop.GetReferenceResolutionInfo()
		if info == nil {
			break
		}
		docs = append(docs, info.AbsoluteDocumentPath)
		hop = info.Object
	}
	return docs
}

// schemaHops follows a resolved schema reference through each document its
// resolution read, js's own first.
//
// GetReferenceChain, read off the fully resolved schema, already starts with js
// itself — js is the chain's top-level entry by construction (GetTopLevelReference
// says so) — so this walks the chain alone rather than prepending js.GetAbsRef()
// to it: doing both read js's own document twice, once directly and once as
// chain[0], for every resolved schema reference including an unchained one.
func schemaHops(js *oas3.JSONSchema[oas3.Referenceable]) []string {
	if !js.IsReference() || !js.IsResolved() {
		return nil
	}
	chain := js.GetResolvedSchema().GetReferenceChain()
	docs := make([]string, 0, min(len(chain), maxResolutionHops))
	for _, e := range chain {
		if len(docs) >= maxResolutionHops {
			break
		}
		docs = append(docs, e.Schema.GetAbsRef().GetURI())
	}
	return docs
}
