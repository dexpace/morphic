package openapi

import (
	soa "github.com/speakeasy-api/openapi/openapi"

	"github.com/dexpace/morphic/ir"
)

// lowerMeta lowers the document-level metadata that is not part of the type or
// service graph: info, servers, and top-level extensions (ir-design §10, §12).
func (l *lowerer) lowerMeta() {
	l.lowerInfo()
	l.lowerServers()
	if ext, diags := extensionsFrom(l.doc.GetExtensions()); len(ext) > 0 {
		l.out.Extensions = mergeExtensions(l.out.Extensions, ext)
		l.diags = append(l.diags, diags...)
	}
}

// lowerInfo maps info onto the document identity, docs, contact, and license.
// GetInfo always returns a non-nil Info (it addresses an embedded struct value),
// so no nil guard is needed.
func (l *lowerer) lowerInfo() {
	info := l.doc.GetInfo()
	l.out.Name = info.GetTitle()
	l.out.Version = info.GetVersion()
	l.out.TermsOfService = info.GetTermsOfService()
	l.out.Docs = l.infoDocs(info)
	if c := info.GetContact(); c != nil {
		l.out.Contact = &ir.Contact{Name: c.GetName(), URL: c.GetURL(), Email: c.GetEmail()}
	}
	if lic := info.GetLicense(); lic != nil {
		l.out.License = &ir.License{Name: lic.GetName(), Identifier: lic.GetIdentifier(), URL: lic.GetURL()}
	}
}

// infoDocs builds the document docs from info summary and description, folding
// in the root externalDocs link when present.
func (l *lowerer) infoDocs(info *soa.Info) ir.Docs {
	d := ir.Docs{Summary: info.GetSummary(), Description: info.GetDescription()}
	if ed := l.doc.GetExternalDocs(); ed != nil {
		d.ExternalDocs = append(d.ExternalDocs, ir.Link{URL: ed.GetURL(), Description: ed.GetDescription()})
	}
	return d
}

// lowerServers lowers the document's servers in source order, each with its URL
// template, description, and templated variables (ir-design §10).
func (l *lowerer) lowerServers() {
	// GetServers never returns an empty slice — it injects a default "/" server
	// when none are declared — so the loop always runs at least once.
	servers := l.doc.GetServers()
	out := make([]ir.Server, 0, len(servers))
	for _, s := range servers {
		if s == nil {
			continue
		}
		out = append(out, lowerServer(s))
	}
	if len(out) > 0 {
		l.out.Servers = out
	}
}

// lowerServer lowers one server. The OpenAPI 3.2 server name, when present,
// becomes the server's naming.
func lowerServer(s *soa.Server) ir.Server {
	srv := ir.Server{
		URLTemplate: s.GetURL(),
		Description: ir.Docs{Description: s.GetDescription()},
		Variables:   serverVariables(s),
	}
	if name := s.GetName(); name != "" {
		srv.Name = ir.Naming{Source: name, Canonical: canonicalWords(name)}
	}
	return srv
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
