package openapi

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// unnamedServerHint names a server whose URL template holds no word to derive a
// hint from. That is "/" in practice — the server GetServers injects for a
// document declaring none — and it is most of the corpus.
//
// The word says what the entity is, which is what a hint is for. The shared mint
// for a name the source left out would read as an omission instead, and here
// there is none: the document named no server because below 3.2 it cannot, and
// its URL is simply the root.
const unnamedServerHint = "server"

// docMeta is the document-level metadata: what ir.Document carries that belongs
// to neither the type graph nor the service graph.
//
// It exists so the lowering can return what it read instead of writing into the
// document as it goes. The caller owns the document and does the assigning,
// which is what lets every function below be checked against a literal context
// and nothing else.
type docMeta struct {
	Name           string
	Version        string
	TermsOfService string
	Docs           ir.Docs
	Contact        *ir.Contact
	License        *ir.License
	Servers        []ir.Server
	Unmodeled      ir.Unmodeled
}

// lowerMeta lowers the document-level metadata that is not part of the type or
// service graph: info, servers, and top-level extensions (ir-design §10, §12).
func lowerMeta(c lowering.Ctx) (docMeta, []ir.Diagnostic) {
	m := lowerInfo(c)
	m.Servers = lowerServers(c)

	ext, diags := annotation.ExtensionsFrom(c.Doc.GetExtensions(), c.SrcIndex, "")
	m.Unmodeled = ext
	return m, diags
}

// lowerInfo maps info onto the document identity, docs, contact, and license.
// GetInfo always returns a non-nil Info (it addresses an embedded struct value),
// so no nil guard is needed.
func lowerInfo(c lowering.Ctx) docMeta {
	info := c.Doc.GetInfo()
	m := docMeta{
		Name:           info.GetTitle(),
		Version:        info.GetVersion(),
		TermsOfService: info.GetTermsOfService(),
		Docs:           infoDocs(c, info),
	}
	if ct := info.GetContact(); ct != nil {
		m.Contact = &ir.Contact{Name: ct.GetName(), URL: ct.GetURL(), Email: ct.GetEmail()}
	}
	if lic := info.GetLicense(); lic != nil {
		m.License = &ir.License{Name: lic.GetName(), Identifier: lic.GetIdentifier(), URL: lic.GetURL()}
	}
	return m
}

// infoDocs builds the document docs from info summary and description, folding
// in the root externalDocs link when present.
func infoDocs(c lowering.Ctx, info *soa.Info) ir.Docs {
	d := ir.Docs{Summary: info.GetSummary(), Description: info.GetDescription()}
	if ed := c.Doc.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
	}
	return d
}

// lowerServers lowers the document's servers in source order, each with its URL
// template, description, and templated variables (ir-design §10). It returns nil
// rather than an empty slice when every entry was skipped, so a document
// declaring no usable server leaves the field unset.
func lowerServers(c lowering.Ctx) []ir.Server {
	// GetServers never returns an empty slice — it injects a default "/" server
	// when none are declared — so the loop always runs at least once.
	servers := c.Doc.GetServers()
	out := make([]ir.Server, 0, len(servers))
	for _, s := range servers {
		if s == nil {
			continue
		}
		out = append(out, lowerServer(s))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lowerServer lowers one server, named by serverName.
func lowerServer(s *soa.Server) ir.Server {
	return ir.Server{
		Name:        serverName(s),
		URLTemplate: s.GetURL(),
		Description: ir.Docs{Description: s.GetDescription()},
		Variables:   serverVariables(s),
	}
}

// serverName builds a server's neutral naming: the declared name when the source
// carries one, which only OpenAPI 3.2 can, and otherwise a hint derived from the
// URL template that locates it. It is the shape operationName uses one level
// down — an entity the source did not name is named by what locates it — and it
// closes the gap where every server below 3.2 reached the IR with all three name
// channels empty (GitHub #258).
//
// The URL rather than the position in servers[]: an index-derived hint says
// nothing about the server it names and stops naming the same one as soon as the
// list is reordered. The whole template rather than a chosen part of it — the
// host, the path — because choosing is a policy about what a server's name is,
// and transcribing needs none: two servers that differ at all differ somewhere
// in it, so the hint distinguishes exactly the servers that are distinct.
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
func serverVariables(s *soa.Server) []ir.ServerVariable {
	vars := s.GetVariables()
	if vars == nil || vars.Len() == 0 {
		return nil
	}
	out := make([]ir.ServerVariable, 0, vars.Len())
	for name, v := range vars.All() {
		if v == nil {
			continue
		}
		out = append(out, ir.ServerVariable{
			Name:    name,
			Default: v.GetDefault(),
			Enum:    v.GetEnum(),
			Docs:    ir.Docs{Description: v.GetDescription()},
		})
	}
	return out
}
