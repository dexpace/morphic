package load

import (
	"context"
	"encoding/json/jsontext"
	"iter"
	"maps"
	"slices"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	"github.com/speakeasy-api/openapi/marshaller"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/sequencedmap"
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// The minors from which OpenAPI stops requiring a field the library requires
// at every version (relaxed).
const (
	responsesOptionalSince   = "3.1" // an Operation Object's responses
	descriptionOptionalSince = "3.2" // a Response Object's description
)

// filledDefault stands in for a server variable's empty default when its
// server's URL check is run again (emptyDefaultEchoes). Any value but the
// empty string is one the check reads as a default.
const filledDefault = "default"

// requiredArtifacts holds what the library's REQUIRED-field rule finds that the
// declared version's specification does not (GitHub #622), dropped wherever
// the library makes it, as dropped's findings are.
//
// The rule departs from the specification twice. It requires at every version
// a field a later minor stopped requiring (relaxed). And it reads a field
// written as the empty string, or an empty map, as missing, where every minor's
// schema admits the value (admittedEmpty). The server URL check repeats the
// second reading for a variable's default (emptyDefaultEchoes).
type requiredArtifacts struct {
	// at holds the nodes the rule's findings are artifacts at.
	at map[*yaml.Node]bool
	// echoes holds the URL check's findings only an empty default draws.
	echoes map[echo]bool
	// fault is the panic that stopped the walk at site, which excuses nothing.
	fault error
	site  jsontext.Pointer
}

// echo identifies a finding of a server's URL check by its node and what it
// says: the check anchors every finding at the url, one per variable.
type echo struct {
	node    *yaml.Node
	message string
}

// excuses reports whether verr is one of the artifacts.
func (r requiredArtifacts) excuses(verr validation.Error) bool {
	switch verr.Rule {
	case validation.RuleValidationRequiredField:
		return r.at[verr.Node]
	case validation.RuleValidationInvalidSyntax:
		return r.echoes[echo{node: verr.Node, message: validationMessage(verr)}]
	default:
		return false
	}
}

// without returns errs less the artifacts among them. errs is not modified.
func (r requiredArtifacts) without(errs []error) []error {
	return slices.DeleteFunc(slices.Clone(errs), func(err error) bool {
		verr, ok := asValidationError(err)
		return ok && r.excuses(verr)
	})
}

// reportedAt returns r with a panic that stopped its walk reported at site
// rather than at the model the walk reached.
func (r requiredArtifacts) reportedAt(site jsontext.Pointer) requiredArtifacts {
	r.site = site
	return r
}

// faults reports the panic that stopped the walk, placed by at, or nothing.
func (r requiredArtifacts) faults(at func(jsontext.Pointer) ir.Provenance) []ir.Diagnostic {
	if r.fault == nil {
		return nil
	}
	return []ir.Diagnostic{diag.Newf(ir.SeverityError, diag.Validation, at(r.site), "%s", r.fault.Error())}
}

// findRequiredArtifacts returns the artifacts among found, the findings about
// the models items reaches, in a document declaring version; opts are what its
// validation hands each object (documentOptions). Where found holds no finding
// of the rule, items is not walked: an empty default's URL echo never comes
// alone, since the default draws the rule's finding beside it.
//
// A node is excused only where no model the walk reads there may hold a finding
// of its own (requiredClaims), and a walk that panics excuses nothing, since a
// model it never reached may hold one.
func findRequiredArtifacts(ctx context.Context, found []error, items iter.Seq[soa.WalkItem], version string,
	opts []validation.Option,
) requiredArtifacts {
	if !slices.ContainsFunc(found, ofTheRule) {
		return requiredArtifacts{}
	}
	c := requiredClaims{artifact: map[*yaml.Node]bool{}, own: map[*yaml.Node]bool{}, echoes: map[echo]bool{}}
	site, err := eachModel(items, "required-field check", func(_ jsontext.Pointer, m any) error {
		c.read(ctx, m, version, opts)
		for _, held := range unwalked(m) {
			c.read(ctx, held, version, opts)
		}
		return nil
	})
	if err != nil {
		return requiredArtifacts{fault: err, site: site}
	}
	maps.DeleteFunc(c.artifact, func(n *yaml.Node, _ bool) bool { return c.own[n] })
	return requiredArtifacts{at: c.artifact, echoes: c.echoes}
}

// ofTheRule reports whether err is a finding of the REQUIRED-field rule.
func ofTheRule(err error) bool {
	verr, ok := asValidationError(err)
	return ok && verr.Rule == validation.RuleValidationRequiredField
}

// requiredClaims records whether each node a model is read at holds an
// artifact of the model's (artifact) or may hold a finding of its own (own).
//
// Through a YAML alias one mapping can be read as two kinds, and a missing
// field of either is anchored at the mapping, so every mapping a model is built
// from is claimed. A string field's value is claimed only as an artifact's: an
// alias gives each read its own node, and a merge key shares one only between
// fields of one name, which admittedEmpty admits for every kind or none.
type requiredClaims struct {
	artifact, own map[*yaml.Node]bool
	echoes        map[echo]bool
}

// read claims the nodes m was built from.
func (c requiredClaims) read(ctx context.Context, m any, version string, opts []validation.Option) {
	root, ok := builtFrom(m)
	if !ok {
		return
	}
	if relaxed(m, version) {
		c.artifact[root] = true
	} else {
		c.own[root] = true
	}
	for _, n := range admittedEmpty(m) {
		c.artifact[n] = true
	}
	if s, ok := m.(*soa.Server); ok {
		maps.Copy(c.echoes, emptyDefaultEchoes(ctx, s, opts))
	}
}

// unwalked returns what m holds that the library's walk does not visit, though
// its validation checks it: an OAuth Flows Object's deviceAuthorization flow
// (openapi/walk_security.go).
func unwalked(m any) []any {
	if flows, ok := m.(*soa.OAuthFlows); ok && flows.DeviceAuthorization != nil {
		return []any{flows.DeviceAuthorization}
	}
	return nil
}

// builtFrom returns the mapping m was built from, its alias followed, which is
// where the library anchors a finding of a field m misses
// (marshaller/unmarshaller.go). A model written as a $ref has none: what it
// names is claimed where that is written. Nor has a model with no mapping, such
// as an extension map.
func builtFrom(m any) (*yaml.Node, bool) {
	if w, ok := m.(referenceWrapper); ok && w.IsReference() {
		return nil, false
	}
	r, ok := m.(rootNoder)
	if !ok {
		return nil, false
	}
	root := nodeview.Deref(r.GetRootNode())
	return root, root != nil
}

// relaxed reports whether m misses a field the library requires of it and the
// declared version does not: an Operation's responses, or a Response's
// description. Each is the one field the library requires of its object, so
// the one finding it anchors at the mapping is about that field.
func relaxed(m any, version string) bool {
	switch v := m.(type) {
	case *soa.Operation:
		return !v.GetCore().Responses.Present && atLeast(version, responsesOptionalSince)
	case *soa.ReferencedResponse:
		return !v.GetObject().GetCore().Description.Present && atLeast(version, descriptionOptionalSince)
	default:
		return false
	}
}

// admittedEmpty returns where m writes a field as the empty string, or as an
// empty map, that every minor's schema types as a bare string or map, as the
// library anchors its finding that the field is missing. An enumerated field,
// such as a Parameter's in, is not admitted: no minor's enumeration holds the
// empty string. Neither is a value written null, which the library reads as a
// type mismatch.
func admittedEmpty(m any) []*yaml.Node {
	switch v := m.(type) {
	case *soa.Info:
		c := v.GetCore()
		return blanks(c, c.Title, c.Version)
	case *soa.License:
		return blanks(v.GetCore(), v.GetCore().Name)
	case *soa.Server:
		return blanks(v.GetCore(), v.GetCore().URL)
	case *soa.ServerVariable:
		return blanks(v.GetCore(), v.GetCore().Default)
	case *soa.Tag:
		return blanks(v.GetCore(), v.GetCore().Name)
	case *oas3.ExternalDocumentation:
		return blanks(v.GetCore(), v.GetCore().URL)
	case *soa.ReferencedParameter:
		c := v.GetObject().GetCore()
		return blanks(c, c.Name)
	case *soa.ReferencedResponse:
		c := v.GetObject().GetCore()
		return blanks(c, c.Description)
	case *soa.ReferencedSecurityScheme:
		c := v.GetObject().GetCore()
		return blanks(c, c.Name, c.Scheme, c.OpenIdConnectUrl)
	case *soa.OAuthFlow:
		c := v.GetCore()
		return blanks(c, c.AuthorizationURL, c.TokenURL, c.DeviceAuthorizationURL)
	case *soa.ReferencedRequestBody:
		return emptyContent(v.GetObject())
	default:
		return nil
	}
}

// blanks returns the node of each of fields written as the empty string, as
// validation.NewValueError anchors a finding about it, given its object's core.
func blanks[T string | *string](core rootNoder, fields ...marshaller.Node[T]) []*yaml.Node {
	var out []*yaml.Node
	for _, f := range fields {
		if f.Present && isEmptyString(f.Value) {
			out = append(out, f.GetValueNodeOrRoot(core.GetRootNode()))
		}
	}
	return out
}

// isEmptyString reports whether v holds the empty string. A nil *string, which
// a null leaves, holds none.
func isEmptyString[T string | *string](v T) bool {
	if p, ok := any(v).(*string); ok {
		return p != nil && *p == ""
	}
	return any(v) == any("")
}

// emptyContent returns where rb writes its content as an empty map, which every
// minor's schema admits, on the condition the library reads as missing.
func emptyContent(rb *soa.RequestBody) []*yaml.Node {
	c := rb.GetCore()
	if !c.Content.Present || rb.GetContent().Len() != 0 {
		return nil
	}
	return []*yaml.Node{c.Content.GetValueNodeOrRoot(c.GetRootNode())}
}

// emptyDefaultEchoes returns the findings s's URL check draws only because a
// variable's default is written as the empty string, which the check reads as
// no default (resolveServerVariables, openapi/server.go): what it finds at the
// url, less what it still finds with each such default filled. The two runs are
// one check over one URL, so a finding both draw reads alike in each. The check
// of the URL the empty defaults complete, which that finding cuts short, is not
// run in its place.
func emptyDefaultEchoes(ctx context.Context, s *soa.Server, opts []validation.Option) map[echo]bool {
	if !writesEmptyDefault(s) {
		return nil
	}
	c := s.GetCore()
	url := c.URL.GetValueNodeOrRoot(c.GetRootNode())
	echoes := urlFindings(ctx, serverWith(s, (*soa.ServerVariable).GetDefault), url, opts)
	still := urlFindings(ctx, serverWith(s, filled), url, opts)
	maps.DeleteFunc(echoes, func(e echo, _ bool) bool { return still[e] })
	return echoes
}

// writesEmptyDefault reports whether a variable of s writes its default as the
// empty string.
func writesEmptyDefault(s *soa.Server) bool {
	for v := range s.GetVariables().Values() {
		if hasEmptyDefault(v) {
			return true
		}
	}
	return false
}

// hasEmptyDefault reports whether v writes its default as the empty string.
func hasEmptyDefault(v *soa.ServerVariable) bool {
	return v.GetCore().Default.Present && v.GetDefault() == ""
}

// filled is v's default, or filledDefault where it is written empty.
func filled(v *soa.ServerVariable) string {
	if hasEmptyDefault(v) {
		return filledDefault
	}
	return v.GetDefault()
}

// serverWith returns a copy of s holding a copy of each variable, its default
// as write returns it. Validating the copy sets nothing on s or its variables.
func serverWith(s *soa.Server, write func(*soa.ServerVariable) string) *soa.Server {
	out := *s
	out.Variables = sequencedmap.New[string, *soa.ServerVariable]()
	for name, v := range s.GetVariables().All() {
		cp := *v
		cp.Default = write(v)
		out.Variables.Set(name, &cp)
	}
	return &out
}

// urlFindings validates s under opts and returns its URL check's findings,
// those of the invalid-syntax rule at url.
func urlFindings(ctx context.Context, s *soa.Server, url *yaml.Node, opts []validation.Option) map[echo]bool {
	out := map[echo]bool{}
	for _, err := range s.Validate(ctx, opts...) {
		if verr, ok := asValidationError(err); ok &&
			verr.Rule == validation.RuleValidationInvalidSyntax && verr.Node == url {
			out[echo{node: verr.Node, message: validationMessage(verr)}] = true
		}
	}
	return out
}
