package load

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"iter"
	"slices"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// The OpenAPI objects additions names, titled as the specification titles them.
const (
	openAPIObject        = "OpenAPI Object"
	infoObject           = "Info Object"
	licenseObject        = "License Object"
	componentsObject     = "Components Object"
	serverObject         = "Server Object"
	tagObject            = "Tag Object"
	pathItemObject       = "Path Item Object"
	parameterObject      = "Parameter Object"
	mediaTypeObject      = "Media Type Object"
	encodingObject       = "Encoding Object"
	exampleObject        = "Example Object"
	responseObject       = "Response Object"
	securitySchemeObject = "Security Scheme Object"
	oauthFlowsObject     = "OAuth Flows Object"
	discriminatorObject  = "Discriminator Object"
	xmlObject            = "XML Object"
	referenceObject      = "Reference Object"
)

// addition is one construct an OpenAPI minor after 3.0 added to an object: a
// field, or one value of a field that existed before.
type addition struct {
	object string
	key    string
	value  string // the value added, or "" when the field itself was added
	since  string // the minor that added it
}

// inSchema reports whether the construct is written inside a Schema Object,
// where the library's check of it is the meta-schema's rather than the object
// model's (see metaSchemaVersionArtifacts).
func (a addition) inSchema() bool {
	return a.object == discriminatorObject || a.object == xmlObject
}

// additions is every construct OpenAPI 3.1 and 3.2 added to an object, a
// schema's own keywords aside (GitHub #820). It is the difference between the
// specification's published schemas of consecutive minors, less a Header
// Object's allowReserved: that is the Parameter Object's, which every minor's
// text gives a header though the 3.1 schema omits it, and where it applies is
// GitHub #628's question.
var additions = []addition{
	{openAPIObject, "jsonSchemaDialect", "", "3.1"},
	{openAPIObject, "webhooks", "", "3.1"},
	{openAPIObject, "$self", "", "3.2"},
	{infoObject, "summary", "", "3.1"},
	{licenseObject, "identifier", "", "3.1"},
	{componentsObject, "pathItems", "", "3.1"},
	{componentsObject, "mediaTypes", "", "3.2"},
	{serverObject, "name", "", "3.2"},
	{tagObject, "summary", "", "3.2"},
	{tagObject, "parent", "", "3.2"},
	{tagObject, "kind", "", "3.2"},
	{pathItemObject, "query", "", "3.2"},
	{pathItemObject, "additionalOperations", "", "3.2"},
	{parameterObject, "in", "querystring", "3.2"},
	{parameterObject, "style", "cookie", "3.2"},
	{mediaTypeObject, "$ref", "", "3.2"},
	{mediaTypeObject, "description", "", "3.2"},
	{mediaTypeObject, "itemSchema", "", "3.2"},
	{mediaTypeObject, "itemEncoding", "", "3.2"},
	{mediaTypeObject, "prefixEncoding", "", "3.2"},
	{encodingObject, "encoding", "", "3.2"},
	{encodingObject, "prefixEncoding", "", "3.2"},
	{encodingObject, "itemEncoding", "", "3.2"},
	{exampleObject, "dataValue", "", "3.2"},
	{exampleObject, "serializedValue", "", "3.2"},
	{responseObject, "summary", "", "3.2"},
	{securitySchemeObject, "type", "mutualTLS", "3.1"},
	{securitySchemeObject, "oauth2MetadataUrl", "", "3.2"},
	{securitySchemeObject, "deprecated", "", "3.2"},
	{oauthFlowsObject, "deviceAuthorization", "", "3.2"},
	{discriminatorObject, "defaultMapping", "", "3.2"},
	{xmlObject, "nodeType", "", "3.2"},
	{referenceObject, "summary", "", "3.1"},
	{referenceObject, "description", "", "3.1"},
}

// laterReport is what laterConstructs found: one diagnostic per construct, and
// whether any is written inside a schema.
type laterReport struct {
	diags    []ir.Diagnostic
	inSchema bool
}

// laterConstructs reports each construct a later minor than the document's
// declared version added to the object it is written on (see
// diag.LaterVersionConstruct). The lowering reads every one at every version;
// this is the one place that asks which minor added a construct.
//
// items is the source's walk, which visits each object once, where it is
// written, never through a $ref, whether or not anything lowers it. A document
// another one's reference reaches is not walked: which version governs it is
// GitHub #839's to settle. A panic in the walk is reported at the model reached.
func laterConstructs(at func(jsontext.Pointer) ir.Provenance, items iter.Seq[soa.WalkItem], declared string) laterReport {
	pending := addedAfter(declared)
	if len(pending) == 0 {
		return laterReport{}
	}
	view := nodeview.New()
	var out laterReport
	site, err := eachModel(items, "version check", func(site jsontext.Pointer, model rootNoder) error {
		object, root, ok := laterObject(model)
		if !ok {
			return nil
		}
		if object == openAPIObject {
			site = "" // the walk spells the root "/", which names the key ""
		}
		for _, a := range writtenOn(object, view.MappingPairs(root), pending) {
			out.diags = append(out.diags, laterDiag(at(site.AppendToken(a.key)), a, declared))
			out.inSchema = out.inSchema || a.inSchema()
		}
		return nil
	})
	if err != nil {
		out.diags = append(out.diags, diag.Newf(ir.SeverityError, diag.Validation, at(site), "%s", err.Error()))
	}
	return out
}

// addedAfter returns the additions a minor later than the declared version made,
// or none for a version the compiler does not read, which Load refuses first.
func addedAfter(declared string) []addition {
	return slices.DeleteFunc(slices.Clone(additions), func(a addition) bool { return !before(declared, a.since) })
}

// writtenOn returns the pending additions pairs write, the effective pairs of one
// object. A Media Type Object written as a $ref, which 3.2 allows, is a
// Reference Object: its $ref is the Media Type's addition, and every key beside
// it is the Reference Object's.
func writtenOn(object string, pairs []nodeview.Pair, pending []addition) []addition {
	asReference := object == mediaTypeObject &&
		slices.ContainsFunc(pairs, func(p nodeview.Pair) bool { return p.Key == "$ref" })
	var out []addition
	for _, pair := range pairs {
		owner := object
		if asReference && pair.Key != "$ref" {
			owner = referenceObject
		}
		for _, a := range pending {
			if a.object == owner && a.key == pair.Key && a.writtenAs(pair.Val) {
				out = append(out, a)
			}
		}
	}
	return out
}

// before reports whether declared is a version the compiler reads whose minor
// is older than since, a minor in supportedMinors.
func before(declared, since string) bool {
	minor, ok := SupportedMinor(declared)
	return ok && slices.Index(supportedMinors, minor) < slices.Index(supportedMinors, since)
}

// writtenAs reports whether val is a value the addition names: any value for an
// added field, the one scalar for an added value.
func (a addition) writtenAs(val *yaml.Node) bool {
	if a.value == "" {
		return true
	}
	n := nodeview.Deref(val)
	return n != nil && n.Kind == yaml.ScalarNode && n.Value == a.value
}

// laterDiag reports one construct, at the key that writes it.
func laterDiag(at ir.Provenance, a addition, declared string) ir.Diagnostic {
	what := fmt.Sprintf("field %q", a.key)
	if a.value != "" {
		what = fmt.Sprintf("value %q of field %q", a.value, a.key)
	}
	return diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, at,
		"%s of the %s was added in OpenAPI %s, and this document declares %s; it is lowered as in an OpenAPI %s document",
		what, a.object, a.since, declared, a.since)
}

// rootNoder is a walked model's view of the mapping it was built from.
type rootNoder interface{ GetRootNode() *yaml.Node }

// referenceWrapper is a model that may be written as a $ref, whose root node is
// then the target's, read where the target is written. A schema is one too.
type referenceWrapper interface {
	rootNoder
	IsReference() bool
}

// laterObject names the object a walked model is, for the objects additions
// has rows for, with the mapping its keys are written on. A model written as a
// $ref is judged on the keys beside it (referenceOf), never on its target's.
func laterObject(m rootNoder) (string, *yaml.Node, bool) {
	object := plainObject(m)
	if w, ok := m.(referenceWrapper); ok {
		if w.IsReference() {
			return referenceOf(w)
		}
		object = wrappedObject(w)
	}
	if object == "" {
		return "", nil, false
	}
	return object, nodeview.DocumentRoot(m.GetRootNode()), true
}

// referenceOf names the object whose keys a model written as a $ref writes
// beside it, with the mapping they are written on. A Path Item's $ref is a
// field of the Path Item, so the keys beside it are the Path Item's own. A
// schema's is a Reference Object in 3.0, but 3.1 admits any keyword beside it,
// which no row expresses (GitHub #871), so it is judged as none here. Any other
// $ref is the Reference Object's.
func referenceOf(w referenceWrapper) (string, *yaml.Node, bool) {
	switch w.(type) {
	case *soa.ReferencedPathItem:
		return pathItemObject, nodeview.DocumentRoot(ownRoot(w)), true
	case *oas3.JSONSchema[oas3.Referenceable]:
		return "", nil, false
	default:
		return referenceObject, nodeview.DocumentRoot(ownRoot(w)), true
	}
}

// ownRoot is the mapping a $ref is written on, which the reference's core holds:
// the model's own GetRootNode answers with the target's.
func ownRoot(w referenceWrapper) *yaml.Node {
	holder, ok := w.(interface{ GetCoreAny() any })
	if !ok {
		return nil
	}
	core, ok := holder.GetCoreAny().(rootNoder)
	if !ok {
		return nil
	}
	return core.GetRootNode()
}

// wrappedObject names the object a model that may stand for a $ref is, or ""
// for one additions has no rows for.
func wrappedObject(model referenceWrapper) string {
	switch model.(type) {
	case *soa.ReferencedPathItem:
		return pathItemObject
	case *soa.ReferencedParameter:
		return parameterObject
	case *soa.ReferencedExample:
		return exampleObject
	case *soa.ReferencedResponse:
		return responseObject
	case *soa.ReferencedSecurityScheme:
		return securitySchemeObject
	default:
		return ""
	}
}

// plainObject names the object a model no $ref can stand for is, or "" for one
// additions has no rows for.
func plainObject(model rootNoder) string {
	switch model.(type) {
	case *soa.OpenAPI:
		return openAPIObject
	case *soa.Info:
		return infoObject
	case *soa.License:
		return licenseObject
	case *soa.Components:
		return componentsObject
	case *soa.Server:
		return serverObject
	case *soa.Tag:
		return tagObject
	case *soa.MediaType:
		return mediaTypeObject
	case *soa.Encoding:
		return encodingObject
	case *soa.OAuthFlows:
		return oauthFlowsObject
	case *oas3.Discriminator:
		return discriminatorObject
	case *oas3.XML:
		return xmlObject
	default:
		return ""
	}
}

// ownAdditionalOperations takes over what the library does with a Path Item's
// additionalOperations in a document older than its model (modelMinor): it
// refuses the field and checks nothing inside it. The compiler lowers the field
// at every version and laterConstructs reports it, so the refusal is dropped and
// the library's own checks are run on every path item whose model holds such
// operations, whether or not the library refused the field: it refuses only one
// it read whole. items is doc's walk; a panic in it or in a check is returned
// as an error among the findings.
func ownAdditionalOperations(ctx context.Context, doc *soa.OpenAPI, items iter.Seq[soa.WalkItem], valErrs []error) []error {
	if !before(doc.OpenAPI, modelMinor()) {
		return valErrs // the library checked the field itself
	}
	held := map[*yaml.Node]bool{}
	var skipped []error
	_, err := eachModel(items, "additionalOperations check",
		func(_ jsontext.Pointer, rp *soa.ReferencedPathItem) error {
			if rp.IsReference() {
				return nil // checked where it is written
			}
			pi := rp.GetObject()
			if pi == nil || pi.AdditionalOperations == nil {
				return nil
			}
			core := pi.GetCore()
			held[core.AdditionalOperations.GetValueNodeOrRoot(core.GetRootNode())] = true
			skipped = append(skipped, additionalOperationsChecks(ctx, doc, pi)...)
			return nil
		})
	if len(held) == 0 && err == nil {
		return valErrs
	}
	out := slices.DeleteFunc(slices.Clone(valErrs), func(ve error) bool {
		verr, ok := asValidationError(ve)
		return ok && verr.Rule == validation.RuleValidationSupportedVersion && held[verr.Node]
	})
	out = append(out, skipped...)
	if err != nil {
		out = append(out, err)
	}
	validation.SortValidationErrors(out)
	return out
}

// modelMinor is the minor the library's model is current to, from which it
// checks a Path Item's additionalOperations (openapi/paths.go). A library that
// moves it must be read again before this compiler follows.
func modelMinor() string {
	minor, _ := SupportedMinor(soa.Version)
	return minor
}

// additionalOperationsChecks is what the library checks of a Path Item's
// additionalOperations at 3.2 (openapi/paths.go): each operation's own
// validation, and a key naming a method that has a field of its own. The
// finding repeats the library's, so a document reads the same at any version.
func additionalOperationsChecks(ctx context.Context, doc *soa.OpenAPI, pi *soa.PathItem) []error {
	opts := documentOptions(doc)
	core := pi.GetCore()
	var errs []error
	for method, op := range pi.AdditionalOperations.All() {
		errs = append(errs, op.Validate(ctx, opts...)...)
		if soa.IsStandardMethod(strings.ToLower(method)) {
			errs = append(errs, validation.NewMapKeyError(validation.SeverityError,
				validation.RuleValidationAllowedValues,
				fmt.Errorf("pathItem.additionalOperations method [%s] is a standardized HTTP method "+
					"and must be defined in its own field", method),
				core, core.AdditionalOperations, method))
		}
	}
	return errs
}

// documentOptions are the options a document's validation hands each object
// it validates (openapi/openapi.go): the document, and its version.
func documentOptions(doc *soa.OpenAPI) []validation.Option {
	version := doc.OpenAPI
	return []validation.Option{
		validation.WithContextObject(doc),
		validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version}),
	}
}
