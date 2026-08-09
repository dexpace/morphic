package openapi

import (
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/ir"
)

// unnamedServerHint names a server whose URL template holds no word to derive a
// hint from — "/" in practice, the server GetServers injects for a document
// declaring none. The word says what the entity is; the shared mint for a name
// the source left out would report an omission that never happened, since below
// 3.2 a document has no way to name a server at all.
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
// service graph: info, servers, and the extensions of every object around them
// that lowers to no node of its own (ir-design §10, §12).
func lowerMeta(c lowering.Ctx) (docMeta, []ir.Diagnostic) {
	m := lowerInfo(c)
	ext, diags := documentExtensions(c)
	m.Unmodeled = ext

	servers, serverDiags := lowerServers(c)
	m.Servers = servers
	diags = append(diags, serverDiags...)
	// After the extensions assignment, which would otherwise overwrite the map.
	return m, append(diags, documentUnknownKeys(c, &m.Unmodeled)...)
}

// documentExtensions collects the x-* of every object that lowers to no IR node
// of its own: the document root, the info block and the contact and license
// inside it, the root externalDocs, the components object, and each declared
// tag with its own externalDocs. ir.Document is the nearest node with an
// Unmodeled map for all of them, so each object's entries are keyed under the
// source path it was written at — see annotation.ExtensionsUnder for why one
// unscoped key for all of them would not do.
func documentExtensions(c lowering.Ctx) (ir.Unmodeled, []ir.Diagnostic) {
	return annotation.ExtensionsAt(c.SrcIndex, append(rootExtensions(c), tagExtensions(c)...)...)
}

// rootExtensions returns the extension sites a document has exactly one of. The
// root's own take no scope, since ir.Document stands for the OpenAPI Object
// itself; the rest are keyed by the path from it down to the object that wrote
// them.
func rootExtensions(c lowering.Ctx) []annotation.ExtensionSite {
	info := c.Doc.GetInfo()
	infoPtr := ids.Ptr("info")
	return []annotation.ExtensionSite{
		{Scope: "", Owner: "", Ext: c.Doc.GetExtensions()},
		{Scope: "info", Owner: infoPtr, Ext: info.GetExtensions()},
		{Scope: "info/contact", Owner: infoPtr + ids.Ptr("contact"), Ext: info.GetContact().GetExtensions()},
		{Scope: "info/license", Owner: infoPtr + ids.Ptr("license"), Ext: info.GetLicense().GetExtensions()},
		{Scope: "externalDocs", Owner: ids.Ptr("externalDocs"), Ext: c.Doc.GetExternalDocs().GetExtensions()},
		{Scope: "components", Owner: ids.Ptr("components"), Ext: c.Doc.GetComponents().GetExtensions()},
	}
}

// tagExtensions returns the extension sites each declared tag contributes: its
// own, and its externalDocs object's. ir.TagDef holds no Unmodeled map and
// neither does ir.Link, so both ride on the document.
//
// Keyed by the tag's index rather than its name, which is the pointer the tag
// is written at. A name would read better and is what TagDefs are found by, but
// OpenAPI's requirement that tag names be unique is the document's to keep, not
// this compiler's to rely on: two tags spelled alike would silently leave one
// entry.
func tagExtensions(c lowering.Ctx) []annotation.ExtensionSite {
	tags := c.Doc.GetTags()
	out := make([]annotation.ExtensionSite, 0, 2*len(tags))
	for i, t := range tags {
		if t == nil {
			continue
		}
		scope := "tags/" + strconv.Itoa(i)
		ptr := ids.Ptr("tags", strconv.Itoa(i))
		out = append(out,
			annotation.ExtensionSite{Scope: scope, Owner: ptr, Ext: t.GetExtensions()},
			annotation.ExtensionSite{Scope: scope + "/externalDocs", Owner: ptr + ids.Ptr("externalDocs"),
				Ext: t.GetExternalDocs().GetExtensions()})
	}
	return out
}

// documentUnknownKeys collects the keys the OpenAPI model names no field for
// from every object around the document metadata that lowers to no node of its
// own: the document root, the info block and the contact and license inside it,
// the root externalDocs, and each declared tag.
//
// ir.Document is the nearest node with an Unmodeled map for all of them, so each
// object's keys are scoped by the source path they were written at. One unscoped
// "openapi:status" would be a single key for six objects, and the entry that
// survived would be whichever site ran last.
func documentUnknownKeys(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	sites := append(rootUnknownSites(c), tagUnknownSites(c)...)
	diags := make([]ir.Diagnostic, 0, len(sites))
	for _, site := range sites {
		diags = append(diags,
			annotation.UnknownKeysUnder(p, site.model, c.SrcIndex, site.owner, site.scope)...)
	}
	return diags
}

// unknownSite is one object's census: what it keys under on the carrier holding
// it, the object's own source pointer, and the parsed object itself.
type unknownSite struct {
	scope string
	owner string
	model any
}

// rootUnknownSites returns the census sites a document has exactly one of. The
// root's keys take no scope, since ir.Document stands for the OpenAPI Object
// itself; the rest are keyed by the path from it down to the object that wrote
// them.
func rootUnknownSites(c lowering.Ctx) []unknownSite {
	info := c.Doc.GetInfo()
	infoPtr := ids.Ptr("info")
	return []unknownSite{
		{"", "", c.Doc},
		{"info", infoPtr, info},
		{"info/contact", infoPtr + ids.Ptr("contact"), info.GetContact()},
		{"info/license", infoPtr + ids.Ptr("license"), info.GetLicense()},
		{"externalDocs", ids.Ptr("externalDocs"), c.Doc.GetExternalDocs()},
	}
}

// tagUnknownSites returns one census site per declared tag, since ir.TagDef
// holds no Unmodeled map for a tag's own keys to land on.
//
// Scoped by index rather than name, which is the pointer a tag is written at. A
// name would read better, but OpenAPI's requirement that tag names be unique is
// the document's to keep and not this compiler's to rely on: two tags spelled
// alike would silently leave one entry.
func tagUnknownSites(c lowering.Ctx) []unknownSite {
	tags := c.Doc.GetTags()
	out := make([]unknownSite, 0, len(tags))
	for i, t := range tags {
		index := strconv.Itoa(i)
		out = append(out, unknownSite{"tags/" + index, ids.Ptr("tags", index), t})
	}
	return out
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
func lowerServers(c lowering.Ctx) ([]ir.Server, []ir.Diagnostic) {
	// GetServers never returns an empty slice — it injects a default "/" server
	// when none are declared — so the loop always runs at least once.
	servers := c.Doc.GetServers()
	out := make([]ir.Server, 0, len(servers))
	var diags []ir.Diagnostic
	for i, s := range servers {
		if s == nil {
			continue
		}
		one, serverDiags := lowerServer(c, s, ids.Ptr("servers", strconv.Itoa(i)))
		diags = append(diags, serverDiags...)
		out = append(out, one)
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// lowerServer lowers one server, named by serverName, keeping its x-* on the
// ir.Server itself; sptr is the server's own pointer in the servers list.
func lowerServer(c lowering.Ctx, s *soa.Server, sptr string) (ir.Server, []ir.Diagnostic) {
	vars, diags := serverVariables(c, s, sptr)
	ext, extDiags := annotation.ExtensionsFrom(s.GetExtensions(), c.SrcIndex, sptr)
	out := ir.Server{
		Name:        serverName(s),
		URLTemplate: s.GetURL(),
		Description: ir.Docs{Description: s.GetDescription()},
		Variables:   vars,
		Unmodeled:   ext,
	}
	diags = append(diags, extDiags...)
	return out, append(diags, annotation.UnknownKeysIn(&out.Unmodeled, s, c.SrcIndex, sptr)...)
}

// serverName builds a server's neutral naming: the declared name when the source
// carries one, which only OpenAPI 3.2 can, else a hint derived from the URL
// template that locates it — the shape operationName already uses for an
// operation with no operationId. Every server below 3.2 used to reach the IR with
// all three name channels empty (GitHub #258).
//
// The URL rather than the position in servers[], which stops naming the same
// server the moment the list is reordered. The whole template rather than a
// chosen part of it, because choosing is a policy about what a server's name is
// where transcribing is not, and it keeps what the omitted part distinguished:
// one host serving /v1 and /v2 is two servers.
//
// Distinct servers can still collide, since canonicalizing drops every non-word
// character and ".../v1" and ".../v-1" reduce to one word sequence. That is the
// collision "red" and "Red" already produce between two enum members, and
// uniquifying it is an emitter's job.
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
func serverVariables(c lowering.Ctx, s *soa.Server, sptr string) ([]ir.ServerVariable, []ir.Diagnostic) {
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
		// field is read directly — as XMLHints already reads its own.
		ext, extDiags := annotation.ExtensionsFrom(v.Extensions, c.SrcIndex, vptr)
		diags = append(diags, extDiags...)
		one := ir.ServerVariable{
			Name:      name,
			Default:   v.GetDefault(),
			Enum:      v.GetEnum(),
			Docs:      ir.Docs{Description: v.GetDescription()},
			Unmodeled: ext,
		}
		diags = append(diags, annotation.UnknownKeysIn(&one.Unmodeled, v, c.SrcIndex, vptr)...)
		out = append(out, one)
	}
	return out, diags
}
