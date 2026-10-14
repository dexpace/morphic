package openapi

import (
	"encoding/json/jsontext"
	"strconv"

	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/compilers/compile"
	"github.com/dexpace/morphic/compilers/openapi/internal/annotation"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/ids"
	"github.com/dexpace/morphic/compilers/openapi/internal/lowering"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
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

// retainedSections are the components sections with no registry. Schemas and
// security schemes are lowered whole and unconditionally; an entry of one of
// these reaches the IR only where a lowering follows a reference to it.
// components/mediaTypes is kept whole instead until a reference into it is
// followed (GitHub #856).
func retainedSections() []string {
	return []string{"responses", "parameters", "examples", "requestBodies", "headers", "links", "callbacks", "pathItems"}
}

// retainUnreferencedComponents keeps every entry of a retainedSections section
// that no lowering resolved a reference to verbatim on the document, under
// openapi:components/<section>/<name> with ReasonNoIRHome and no diagnostic
// (GitHub #616). It must run after every other lowering: only they know what
// they followed (lowering.Ctx.Reached), and a $ref string they never follow,
// in a links map, an x- extension or an example's value, carries nothing of
// its target into the IR. A section is read as the parser reads it, through
// `<<` merge keys, so a merged-in entry is kept under its own name.
func retainUnreferencedComponents(c lowering.Ctx) (ir.Unmodeled, []ir.Diagnostic) {
	view := nodeview.New()
	components := view.ChildByToken(nodeview.DocumentRoot(c.Doc.GetRootNode()), "components")
	var out ir.Unmodeled
	var diags []ir.Diagnostic
	for _, section := range retainedSections() {
		for _, entry := range view.MappingPairs(view.ChildByToken(nodeview.Deref(components), section)) {
			pointer := ids.Ptr("components", section, entry.Key)
			if c.Reached(pointer) {
				continue
			}
			_, keptDiags := annotation.PreserveNodeInto(&out, "openapi:"+ids.Scope("components", section, entry.Key),
				entry.Val, ir.ReasonNoIRHome, c.ProvenanceAt(pointer))
			diags = append(diags, keptDiags...)
		}
	}
	if view.Exhausted() {
		diags = append(diags, c.DiagAt(ir.SeverityWarning, diag.DegradedConstruct, ids.Ptr("components"),
			"components merges through `<<` deeper than %d levels; entries past that are not kept", nodeview.MergeDepthLimit))
	}
	return out, diags
}

// lowerMeta lowers the document-level metadata that is not part of the type or
// service graph: info, servers, and the extensions of every object around them
// that lowers to no node of its own (ir-design §10, §12).
func lowerMeta(c lowering.Ctx) (docMeta, []ir.Diagnostic) {
	m := lowerInfo(c)

	ext, diags := documentExtensions(c)
	m.Unmodeled = ext

	retained, retainDiags := retainUnreferencedComponents(c)
	m.Unmodeled = annotation.MergeUnmodeled(m.Unmodeled, retained)
	diags = append(diags, retainDiags...)

	servers, serverDiags := lowerServers(c)
	m.Servers = servers
	diags = append(diags, serverDiags...)

	diags = append(diags, keepComponentMediaTypes(c, &m.Unmodeled)...)
	diags = append(diags, keepJSONSchemaDialect(c, &m.Unmodeled)...)
	return m, append(diags, documentUnknownKeys(c, &m.Unmodeled)...)
}

// keepJSONSchemaDialect keeps the document's jsonSchemaDialect verbatim on the
// document, with an info. It is the default `$schema` of the document's Schema
// Objects, and the IR models no dialect, so it is kept as a schema's own
// `$schema` is (annotation.DialectKeywords): ReasonOutOfScope, and every schema
// read in the OpenAPI dialect whatever it names.
func keepJSONSchemaDialect(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	at := ids.Ptr("jsonSchemaDialect")
	node := nodeview.New().ChildByToken(nodeview.DocumentRoot(c.Doc.GetRootNode()), "jsonSchemaDialect")
	kept, diags := annotation.PreserveNodeInto(p, annotation.EntryKey("", "jsonSchemaDialect"), node,
		ir.ReasonOutOfScope, c.ProvenanceAt(at))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"jsonSchemaDialect names the JSON Schema dialect of the document's schemas; the IR models no "+
			"dialect, so it is kept verbatim under Unmodeled and every schema is read in the OpenAPI dialect"))
}

// keepComponentMediaTypes keeps the components/mediaTypes map OpenAPI 3.2 adds
// verbatim on the document, read as the parser reads it, at the entry and
// pointer the components census would use, so the census leaves it alone. The
// parser models no such section and this compiler does not resolve a reference
// into it yet (GitHub #856), so the map has no IR home. It is kept at every
// version, as load reports it below 3.2 (diag.LaterVersionConstruct).
func keepComponentMediaTypes(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	view := nodeview.New()
	components := view.ChildByToken(nodeview.DocumentRoot(c.Doc.GetRootNode()), "components")
	at := ids.Ptr("components", "mediaTypes")
	kept, diags := annotation.PreserveNodeInto(p, annotation.EntryKey("components", "mediaTypes"),
		view.ChildByToken(nodeview.Deref(components), "mediaTypes"), ir.ReasonNoIRHome, c.ProvenanceAt(at))
	if !kept {
		return diags
	}
	return append(diags, c.DiagAt(ir.SeverityInfo, diag.DegradedConstruct, at,
		"components/mediaTypes is not resolved by this compiler yet (GitHub #856); kept verbatim under Unmodeled"))
}

// documentUnknownKeys collects the keys the OpenAPI model names no field for
// from every object around the document metadata that lowers to no node of its
// own: the root, info with its contact and license, the root externalDocs, the
// components object, and each tag with its own externalDocs.
//
// ir.Document is the nearest node with an Unmodeled map for all of them, so each
// object's keys are scoped by the source path they were written at. One unscoped
// "openapi:status" would be a single key for every object, and the entry that
// survived would be whichever site ran last.
func documentUnknownKeys(c lowering.Ctx, p *ir.Unmodeled) []ir.Diagnostic {
	sites := append(rootUnknownSites(c), tagUnknownSites(c)...)
	diags := make([]ir.Diagnostic, 0, len(sites))
	for _, site := range sites {
		diags = append(diags,
			annotation.UnknownKeysUnder(p, site.model, c.ProvenanceAt, site.owner, site.scope)...)
	}
	return diags
}

// unknownSite is one object's census: what it keys under on the carrier holding
// it, the object's own source pointer, and the parsed object itself.
type unknownSite struct {
	scope string
	owner jsontext.Pointer
	model any
}

// rootUnknownSites returns the census sites a document has exactly one of. The
// root's keys take no scope, since ir.Document stands for the OpenAPI Object
// itself; the rest are keyed by the path from it down to the object that wrote
// them.
//
// The components object is one of them, unlike the maps beneath it: `paths`,
// `responses` and a callback each embed a sequenced map, so every key they hold
// is a valid entry, while the Components Object is a fixed-field struct and a
// key it does not define is as undeclared as one on any other object here.
func rootUnknownSites(c lowering.Ctx) []unknownSite {
	info := c.Doc.GetInfo()
	infoPtr := ids.Ptr("info")
	return []unknownSite{
		{"", "", c.Doc},
		{"info", infoPtr, info},
		{"info/contact", infoPtr + ids.Ptr("contact"), info.GetContact()},
		{"info/license", infoPtr + ids.Ptr("license"), info.GetLicense()},
		{"externalDocs", ids.Ptr("externalDocs"), c.Doc.GetExternalDocs()},
		{"components", ids.Ptr("components"), c.Doc.GetComponents()},
	}
}

// tagUnknownSites returns the census sites each declared tag contributes: its
// own, and its externalDocs object's. Neither ir.TagDef nor ir.Link holds an
// Unmodeled map, so both ride on the document.
//
// Scoped by index rather than name, which is the pointer a tag is written at. A
// name would read better, but OpenAPI's requirement that tag names be unique is
// the document's to keep and not this compiler's to rely on: two tags spelled
// alike would silently leave one entry.
func tagUnknownSites(c lowering.Ctx) []unknownSite {
	tags := c.Doc.GetTags()
	out := make([]unknownSite, 0, 2*len(tags))
	for i, t := range tags {
		if t == nil {
			continue
		}
		index := strconv.Itoa(i)
		ptr := ids.Ptr("tags", index)
		out = append(out,
			unknownSite{"tags/" + index, ptr, t},
			unknownSite{"tags/" + index + "/externalDocs", ptr + ids.Ptr("externalDocs"), t.GetExternalDocs()})
	}
	return out
}

// documentExtensions collects the x-* of every object that lowers to no IR node
// of its own: the document root, the info block and the contact and license
// inside it, the root externalDocs, the components object, and each declared
// tag with its own externalDocs. ir.Document is the nearest node with an
// Unmodeled map for all of them, so each object's entries are keyed under the
// source path it was written at — see annotation.ExtensionsUnder for why one
// unscoped key for all of them would not do.
func documentExtensions(c lowering.Ctx) (ir.Unmodeled, []ir.Diagnostic) {
	return annotation.ExtensionsAt(c.ProvenanceAt, append(rootExtensions(c), tagExtensions(c)...)...)
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
//
// A declared name one entry repeats from another is reported and nothing else
// changes: both servers still lower, each with the name the document wrote
// (serverNameClaims).
func lowerServers(c lowering.Ctx) ([]ir.Server, []ir.Diagnostic) {
	// GetServers never returns an empty slice — it injects a default "/" server
	// when none are declared — so the loop always runs at least once.
	servers := c.Doc.GetServers()
	out := make([]ir.Server, 0, len(servers))
	claims := make(serverNameClaims, len(servers))
	var diags []ir.Diagnostic
	for i, s := range servers {
		if s == nil {
			continue
		}
		sptr := ids.Ptr("servers", strconv.Itoa(i))
		one, serverDiags := lowerServer(c, s, sptr)
		diags = append(diags, serverDiags...)
		diags = append(diags, claims.claim(c, s.GetName(), sptr)...)
		out = append(out, one)
	}
	if len(out) == 0 {
		return nil, diags
	}
	return out, diags
}

// serverNameClaims maps each declared server name to the name key of the first
// server that declared it. It is consulted in source order and never iterated,
// so map order cannot reach a diagnostic.
type serverNameClaims map[string]jsontext.Pointer

// claim records name for the server at sptr, or reports it when an earlier
// server already declared it, compared as exact strings. An empty name claims
// nothing, and a URL hint is never a claim: neither is a name the document wrote.
//
// The report sits at the repeat's name key, unless only the earlier claim came
// from the overlay: the overlay then made the two collide, so the report names
// it as the source a reader has to open.
func (claims serverNameClaims) claim(c lowering.Ctx, name string, sptr jsontext.Pointer) []ir.Diagnostic {
	if name == "" {
		return nil
	}
	at := sptr + ids.Ptr("name")
	first, seen := claims[name]
	if !seen {
		claims[name] = at
		return nil
	}
	if c.ProvenanceAt(at).Source == c.SrcIndex && c.ProvenanceAt(first).Source != c.SrcIndex {
		at, first = first, at
	}
	return []ir.Diagnostic{c.DiagAt(ir.SeverityWarning, diag.DuplicateServerName, at,
		"server name %q is also declared by the server at %s; both are kept, and a consumer "+
			"keying servers by name cannot tell them apart", name, first)}
}

// lowerServer lowers one server, named by serverName, keeping its x-* on the
// ir.Server itself; sptr is the server's own pointer in the servers list.
func lowerServer(c lowering.Ctx, s *soa.Server, sptr jsontext.Pointer) (ir.Server, []ir.Diagnostic) {
	vars, diags := serverVariables(c, s, sptr)
	ext, extDiags := annotation.ExtensionsFrom(s.GetExtensions(), c.ProvenanceAt, sptr)
	diags = append(diags, extDiags...)
	out := ir.Server{
		Name:        serverName(s),
		URLTemplate: s.GetURL(),
		Description: ir.Docs{Description: s.GetDescription()},
		Variables:   vars,
		Unmodeled:   ext,
	}
	return out, append(diags, annotation.UnknownKeysIn(&out.Unmodeled, s, c.ProvenanceAt, sptr)...)
}

// serverName builds a server's neutral naming: the declared name (defined by
// OpenAPI 3.2, but read on every version, as the parser does), else a hint
// derived from the whole URL template, as operationName does for an operation
// with no operationId (GitHub #258). Not the position, which shifts on
// reordering, nor part of the URL: one host serving /v1 and /v2 is two servers.
//
// Distinct servers can collide (".../v1" and ".../v-1" reduce to one word
// sequence), as can declared names differing only once canonicalized (prod,
// Prod); an emitter uniquifies. Only exact repeats are reported
// (serverNameClaims).
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
		// field is read directly — as XMLHints already reads its own.
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
