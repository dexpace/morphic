package document

import (
	"encoding/json/jsontext"
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

// serverName builds a server's neutral naming: the declared name (only OpenAPI
// 3.2 has one), else a hint derived from the URL template, as operationName does
// for an operation with no operationId (GitHub #258). It names by the URL, not
// the position in servers[], which shifts when the list is reordered, and by the
// whole template, not a part: choosing a part is a policy and drops what the
// rest distinguishes, since one host serving /v1 and /v2 is two servers.
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
