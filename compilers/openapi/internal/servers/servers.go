package servers

import (
	"encoding/json/jsontext"
	"maps"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// unnamedServerHint names a server whose URL template holds no word to derive a
// hint from — "/" in practice, the server OpenAPI supplies for a document
// declaring none. The word says what the entity is; the shared mint for a name
// the source left out would report an omission that never happened, since below
// 3.2 a document has no way to name a server at all.
const unnamedServerHint = "server"

// defaultServerURL is the URL of the server OpenAPI supplies when the
// document's servers field is absent or empty.
const defaultServerURL = "/"

// Registry is every server lowered so far, keyed by ID, for one compile. A
// declaration is lowered once: a path item two paths mount registers its
// servers at the first mount and names the same IDs at the second, reporting
// nothing twice. The zero value is not usable; NewRegistry builds one.
type Registry struct {
	byID map[ir.ServerID]ir.Server
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byID: map[ir.ServerID]ir.Server{}}
}

// Root registers the document's own servers and returns their IDs in source
// order: the servers its service is sent to. A document whose servers field is
// absent or empty is served at "/", which OpenAPI supplies in its place and
// which is minted at ids.DefaultServer with no pointer, since nothing in the
// source writes it (GitHub #672).
func (r *Registry) Root(c lowering.Ctx) ([]ir.ServerID, []ir.Diagnostic) {
	if declared := documentServers(c.Doc); len(declared) > 0 {
		return r.Declared(c, declared, "")
	}
	id := ids.DefaultServer()
	if _, held := r.byID[id]; !held {
		r.byID[id] = ir.Server{
			ID:          id,
			Name:        serverName(&soa.Server{URL: defaultServerURL}),
			URLTemplate: defaultServerURL,
			Provenance:  c.ProvenanceAt(""),
		}
	}
	return []ir.ServerID{id}, nil
}

// Declared registers the servers list the object at owner declares and returns
// their IDs in source order, or nil when it lowers none. c is the context of the
// document that declares the list, so each server's ID and provenance name its
// own position there, whichever mount reached it.
//
// A nil entry, which no valid document writes, lowers nothing and keeps the
// index it occupies, so the next entry's ID is still its own position.
func (r *Registry) Declared(c lowering.Ctx, list []*soa.Server, owner jsontext.Pointer) ([]ir.ServerID, []ir.Diagnostic) {
	if len(list) == 0 {
		return nil, nil
	}
	out := make([]ir.ServerID, 0, len(list))
	var diags []ir.Diagnostic
	for i, s := range list {
		if s == nil {
			continue
		}
		at := owner + ids.Ptr("servers", strconv.Itoa(i))
		id := c.IDs().Server(at)
		out = append(out, id)
		if _, held := r.byID[id]; held {
			continue
		}
		one, serverDiags := lowerServer(c, s, id, at)
		r.byID[id] = one
		diags = append(diags, serverDiags...)
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// Servers returns a copy of the registry, or nil when nothing was registered,
// so a Document declaring no server leaves the field unset.
func (r *Registry) Servers() map[ir.ServerID]ir.Server {
	if len(r.byID) == 0 {
		return nil
	}
	return maps.Clone(r.byID)
}

// documentServers returns the servers list doc writes, nil for no document. It
// reads the field rather than GetServers, which stands in a "/" server for an
// empty list, so Root can mint that default as no declaration.
func documentServers(doc *soa.OpenAPI) []*soa.Server {
	if doc == nil {
		return nil
	}
	return doc.Servers
}

// lowerServer lowers one server declared at sptr as id, keeping its x-* and
// undeclared keys on the ir.Server itself.
func lowerServer(c lowering.Ctx, s *soa.Server, id ir.ServerID, sptr jsontext.Pointer) (ir.Server, []ir.Diagnostic) {
	vars, diags := serverVariables(c, s, sptr)
	ext, extDiags := annotation.ExtensionsFrom(s.GetExtensions(), c.ProvenanceAt, sptr)
	diags = append(diags, extDiags...)
	out := ir.Server{
		ID:          id,
		Name:        serverName(s),
		URLTemplate: s.GetURL(),
		Description: ir.Docs{Description: s.GetDescription()},
		Variables:   vars,
		Unmodeled:   ext,
		Provenance:  c.ProvenanceAt(sptr),
	}
	return out, append(diags, annotation.UnknownKeysIn(&out.Unmodeled, s, c.ProvenanceAt, sptr)...)
}

// serverName builds a server's neutral naming: the declared name (only OpenAPI
// 3.2 has one), else a hint derived from the URL template, as operation's
// operationName does for an operation with no operationId (GitHub #258). It
// names by the URL, not the position in servers[], which shifts when the list
// is reordered, and by the whole template, not a part: choosing a part is a
// policy and drops what the rest distinguishes, since one host serving /v1 and
// /v2 is two servers.
//
// Distinct servers can collide (".../v1" and ".../v-1" reduce to one word
// sequence), as two enum members can; an emitter uniquifies.
func serverName(s *soa.Server) ir.Naming {
	if name := s.GetName(); name != "" {
		return compile.NamingFor(name)
	}
	if words := ir.CanonicalWords(s.GetURL()); words != "" {
		return compile.NamingHint(words)
	}
	return compile.NamingHint(unnamedServerHint)
}

// serverVariables lowers a server's URL template variables in source order, or
// nil when it declares none.
func serverVariables(c lowering.Ctx, s *soa.Server, sptr jsontext.Pointer) ([]ir.ServerVariable, []ir.Diagnostic) {
	vars := s.GetVariables()
	if vars == nil || vars.Len() == 0 {
		return nil, nil
	}
	out := make([]ir.ServerVariable, 0, vars.Len())
	var diags []ir.Diagnostic
	for name, v := range vars.All() {
		if v == nil {
			continue
		}
		vptr := sptr + ids.Ptr("variables", name)
		// ServerVariable exposes no GetExtensions at this library version, so the
		// field is read directly — as annotation.XMLHints already reads its own.
		ext, extDiags := annotation.ExtensionsFrom(v.Extensions, c.ProvenanceAt, vptr)
		diags = append(diags, extDiags...)
		one := ir.ServerVariable{
			Name:      name,
			Default:   v.GetDefault(),
			Enum:      v.GetEnum(),
			Docs:      ir.Docs{Description: v.GetDescription()},
			Unmodeled: ext,
		}
		diags = append(diags, annotation.UnknownKeysIn(&one.Unmodeled, v, c.ProvenanceAt, vptr)...)
		out = append(out, one)
	}
	return out, diags
}
