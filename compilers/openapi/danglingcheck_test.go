package openapi_test // external test package — exercises only the public API

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/ir"
)

// danglingDir holds the twelve issue-#14 reproducers, copied out of triage so the
// tests are self-contained.
const danglingDir = "../../testdata/dangling/openapi"

// danglingRefs walks a produced Document and returns a sorted, human-readable
// list of every reference that resolves to nothing: a TypeRef, Discriminator, or
// Value whose target TypeID is absent from doc.Types, or a SchemeUse whose AuthID
// is absent from doc.Auth. An empty result means the IR is referentially closed —
// the property issue #14 restores. The walk is exhaustive across every ID-bearing
// site: it descends the type registry (each TypeDef, its nested TypeRefs, wire
// encodings, and example value refs) and the service/operation tree (parameters,
// request and response payloads, content items and file contents, part and
// response headers, error cases, streaming, pagination, long-running, resource
// members, and service renames). Covering every site — not only the ones issue
// #14 touched — is what lets the oracle double as a regression net against a
// dangling reference reintroduced anywhere.
func danglingRefs(doc *ir.Document) []string {
	c := &refChecker{doc: doc}
	for id, td := range doc.Types {
		c.where = string(id)
		c.typeDef(td)
		c.examples(".examples", td.Common().Examples)
	}
	for i, svc := range doc.Services {
		c.service(i, svc)
	}
	sort.Strings(c.out)
	return c.out
}

type refChecker struct {
	doc   *ir.Document
	where string
	out   []string
}

func (c *refChecker) typeID(field string, id ir.TypeID) {
	if id == "" {
		return
	}
	if _, ok := c.doc.Types[id]; !ok {
		c.out = append(c.out, fmt.Sprintf("type %s%s -> %s", c.where, field, id))
	}
}

func (c *refChecker) ref(field string, r ir.TypeRef) { c.typeID(field, r.Target) }

func (c *refChecker) typeDef(td ir.TypeDef) {
	switch t := td.(type) {
	case *ir.Model:
		c.model(t)
	case *ir.Union:
		for i, v := range t.Variants {
			c.ref(fmt.Sprintf(".variants[%d]", i), v.Type)
			c.examples(fmt.Sprintf(".variants[%d].examples", i), v.Examples)
		}
		c.discriminator(t.Discriminator)
	case *ir.Scalar:
		if t.Base != nil {
			c.ref(".base", *t.Base)
		}
		if t.Encoding != nil && t.Encoding.WireType != nil {
			c.ref(".encoding.wireType", *t.Encoding.WireType)
		}
	case *ir.List:
		c.ref(".elem", t.Elem)
		if t.Encoding != nil && t.Encoding.WireType != nil {
			c.ref(".encoding.wireType", *t.Encoding.WireType)
		}
	case *ir.MapT:
		c.ref(".key", t.Key)
		c.ref(".value", t.Value)
	case *ir.Tuple:
		for i, e := range t.Elems {
			c.ref(fmt.Sprintf(".elems[%d]", i), e)
		}
	case *ir.Enum:
		for i, m := range t.Members {
			c.value(fmt.Sprintf(".members[%d]", i), m.Value)
			c.examples(fmt.Sprintf(".members[%d].examples", i), m.Examples)
		}
	case *ir.Literal:
		c.value(".value", t.Value)
	}
}

func (c *refChecker) model(m *ir.Model) {
	if m.Base != nil {
		c.ref(".base", *m.Base)
	}
	for i, mx := range m.Mixins {
		c.ref(fmt.Sprintf(".mixins[%d]", i), mx)
	}
	for i, im := range m.Implements {
		c.ref(fmt.Sprintf(".implements[%d]", i), im)
	}
	for _, p := range m.Properties {
		c.property(".prop."+p.WireName, p)
	}
	if ap := m.AdditionalProps; ap != nil {
		c.ref(".additionalProps.value", ap.Value)
		if ap.Key != nil {
			c.ref(".additionalProps.key", *ap.Key)
		}
		for i, pp := range ap.Patterns {
			c.ref(fmt.Sprintf(".additionalProps.patterns[%d]", i), pp.Value)
		}
	}
	c.discriminator(m.Discriminator)
}

// property walks every ID-bearing site of a Property: its type, default,
// examples, and field arguments (GraphQL argument shapes reuse Parameter).
func (c *refChecker) property(field string, p ir.Property) {
	c.ref(field, p.Type)
	if p.Default != nil {
		c.value(field+".default", *p.Default)
	}
	c.examples(field+".examples", p.Examples)
	for i, a := range p.Args {
		c.param(fmt.Sprintf("%s.args[%d]", field, i), a)
	}
}

// param walks the type, default, and examples of a Parameter.
func (c *refChecker) param(field string, p ir.Parameter) {
	c.ref(field+".type", p.Type)
	if p.Default != nil {
		c.value(field+".default", *p.Default)
	}
	c.examples(field+".examples", p.Examples)
}

func (c *refChecker) discriminator(d *ir.Discriminator) {
	if d == nil {
		return
	}
	for value, id := range d.Mapping {
		c.typeID(".discriminator.mapping."+value, id)
	}
	c.typeID(".discriminator.default", d.Default)
}

func (c *refChecker) value(field string, v ir.Value) {
	if v.Ref != nil {
		c.typeID(field+".ref.type", v.Ref.Type)
	}
	if v.Ctor != nil {
		c.typeID(field+".ctor.scalar", v.Ctor.Scalar)
		for i, a := range v.Ctor.Args {
			c.value(fmt.Sprintf("%s.ctor.args[%d]", field, i), a)
		}
	}
	for i, e := range v.List {
		c.value(fmt.Sprintf("%s[%d]", field, i), e)
	}
	for _, f := range v.Object {
		c.value(field+"."+f.Name, f.Value)
	}
}

// examples walks the value and error refs of a slice of Examples.
func (c *refChecker) examples(field string, exs []ir.Example) {
	for i, ex := range exs {
		p := fmt.Sprintf("%s[%d]", field, i)
		c.optValue(p+".value", ex.Value)
		c.optValue(p+".headers", ex.Headers)
		c.optValue(p+".input", ex.Input)
		c.optValue(p+".output", ex.Output)
		if ex.Error != nil {
			c.ref(p+".error.type", ex.Error.Type)
			c.value(p+".error.content", ex.Error.Content)
		}
	}
}

func (c *refChecker) optValue(field string, v *ir.Value) {
	if v != nil {
		c.value(field, *v)
	}
}

// service walks every ID-bearing site of a Service: its auth default, common
// errors, presentation renames (a TypeID-keyed map), and operation groups.
func (c *refChecker) service(i int, svc ir.Service) {
	c.where = fmt.Sprintf("service[%d]", i)
	c.authReqs(".auth", svc.Auth)
	for j, ec := range svc.CommonErrors {
		c.ref(fmt.Sprintf(".commonErrors[%d].type", j), ec.Type)
	}
	for id := range svc.Renames {
		c.typeID(".renames", id)
	}
	c.groups(svc.Groups)
}

// groups descends the operation-group tree, walking each group's resource
// members, its operations, and its nested groups.
func (c *refChecker) groups(gs []ir.OperationGroup) {
	for _, g := range gs {
		if g.Resource != nil {
			c.where = "group " + g.Name.Source
			for i, p := range g.Resource.Identifiers {
				c.property(fmt.Sprintf(".resource.identifiers[%d]", i), p)
			}
			for i, p := range g.Resource.Properties {
				c.property(fmt.Sprintf(".resource.properties[%d]", i), p)
			}
		}
		for _, op := range g.Operations {
			c.operation(op)
		}
		c.groups(g.Groups)
	}
}

// operation walks every ID-bearing site of an Operation.
func (c *refChecker) operation(op ir.Operation) {
	c.where = "op " + op.Name.Source
	c.authReqs(".auth", op.Auth)
	for i, p := range op.Params {
		c.param(fmt.Sprintf(".params[%d]", i), p)
	}
	c.payload(".request", op.Request)
	for i, r := range op.Responses {
		c.response(fmt.Sprintf(".responses[%d]", i), r)
	}
	for i, ec := range op.Errors {
		c.ref(fmt.Sprintf(".errors[%d].type", i), ec.Type)
	}
	c.stream(".requestStream", op.RequestStream)
	c.stream(".responseStream", op.ResponseStream)
	c.pagination(op.Pagination)
	c.longRunning(op.LongRunning)
	c.examples(".examples", op.Examples)
}

func (c *refChecker) payload(field string, p *ir.Payload) {
	if p == nil {
		return
	}
	for i, ct := range p.Contents {
		c.content(fmt.Sprintf("%s.contents[%d]", field, i), ct)
	}
}

// content walks a Content's type, sequential item, file contents, part
// encodings, and examples.
func (c *refChecker) content(field string, ct ir.Content) {
	c.ref(field+".type", ct.Type)
	if ct.Item != nil {
		c.ref(field+".item", *ct.Item)
	}
	if ct.File != nil && ct.File.Contents != nil {
		c.ref(field+".file.contents", *ct.File.Contents)
	}
	c.partEncodings(field+".itemEncoding", ct.ItemEncoding)
	c.partEncodings(field+".encoding", ct.Encoding)
	c.examples(field+".examples", ct.Examples)
}

func (c *refChecker) partEncodings(field string, m map[string]ir.PartEncoding) {
	for key, pe := range m {
		for i, h := range pe.Headers {
			c.property(fmt.Sprintf("%s.%s.headers[%d]", field, key, i), h)
		}
	}
}

// response walks a Response's payload, headers, and status-code prop path.
func (c *refChecker) response(field string, r ir.Response) {
	c.payload(field+".payload", r.Payload)
	for i, h := range r.Headers {
		c.property(fmt.Sprintf("%s.headers[%d]", field, i), h)
	}
	c.optPropPath(field+".statusCodeProp", r.StatusCodeProp)
}

func (c *refChecker) propPath(field string, p ir.PropPath) {
	if p.Root != nil {
		c.ref(field+".root", *p.Root)
	}
}

func (c *refChecker) optPropPath(field string, p *ir.PropPath) {
	if p != nil {
		c.propPath(field, *p)
	}
}

func (c *refChecker) stream(field string, s *ir.StreamDetail) {
	if s == nil {
		return
	}
	if s.Events != nil {
		c.ref(field+".events", *s.Events)
	}
	if s.Initial != nil {
		c.ref(field+".initial", *s.Initial)
	}
}

// pagination walks the response-body prop paths of a Pagination; its input
// cursors are ParamPaths, which carry no TypeRef.
func (c *refChecker) pagination(p *ir.Pagination) {
	if p == nil {
		return
	}
	c.optPropPath(".pagination.items", p.Items)
	c.optPropPath(".pagination.nextCursor", p.NextCursor)
	c.optPropPath(".pagination.nextLink", p.NextLink)
	c.optPropPath(".pagination.prevLink", p.PrevLink)
	c.optPropPath(".pagination.firstLink", p.FirstLink)
	c.optPropPath(".pagination.lastLink", p.LastLink)
	c.optPropPath(".pagination.totalCount", p.TotalCount)
}

func (c *refChecker) longRunning(l *ir.LongRunning) {
	if l == nil {
		return
	}
	if l.PollingType != nil {
		c.ref(".longRunning.pollingType", *l.PollingType)
	}
	if l.FinalType != nil {
		c.ref(".longRunning.finalType", *l.FinalType)
	}
	c.optPropPath(".longRunning.resultPath", l.ResultPath)
}

// authReqs flags every SchemeUse whose AuthID is absent from doc.Auth.
func (c *refChecker) authReqs(field string, reqs []ir.AuthRequirement) {
	for i, req := range reqs {
		for j, u := range req.Schemes {
			if u.Scheme == "" {
				continue
			}
			if _, ok := c.doc.Auth[u.Scheme]; !ok {
				c.out = append(c.out, fmt.Sprintf("auth %s%s[%d].schemes[%d] -> %s",
					c.where, field, i, j, u.Scheme))
			}
		}
	}
}

// compileFile compiles one spec file through the public compiler, using srcPath
// as the source path (which drives same-file $ref resolution).
func compileFile(t *testing.T, dir, file, srcPath string) (*ir.Document, []ir.Diagnostic) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, file))
	require.NoError(t, err)
	doc, diags, err := openapi.New().Compile(t.Context(),
		[]compilers.Source{{Path: srcPath, Data: data}}, compilers.Options{})
	require.NoError(t, err)
	require.NotNil(t, doc)
	return doc, diags
}

// hasErrorRef reports whether the diagnostics carry an error-severity
// unresolved-ref finding — the signal that an offending entry was dropped.
func hasErrorRef(diags []ir.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == ir.SeverityError && d.Code == "openapi/unresolved-ref" {
			return true
		}
	}
	return false
}

func hasAnyError(diags []ir.Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == ir.SeverityError {
			return true
		}
	}
	return false
}

// outcome classifies how a reproducer must be made referentially sound.
type outcome int

const (
	// drops the offending entry with an error-severity unresolved-ref diagnostic.
	drops outcome = iota
	// interns the target so the reference resolves, with no error diagnostic.
	interns
	// resolves internally but the loader still reports the external file it could
	// not fetch (f12): only referential closure is asserted.
	internsNoisy
)

// TestDanglingRefs_Reproducers compiles each issue-#14 reproducer and asserts the
// produced IR has zero dangling references — every offending entry either interns
// correctly or is dropped with an error-severity diagnostic.
func TestDanglingRefs_Reproducers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file    string
		srcPath string
		want    outcome
	}{
		{"f04-composition.yaml", "f04.yaml", drops},
		{"f05-discriminator.yaml", "f05.yaml", drops},
		{"f06-discriminator.yaml", "f06.yaml", drops},
		{"f07-discriminator.yaml", "f07.yaml", interns},
		{"f08-discriminator.yaml", "f08.yaml", drops},
		{"f09-discriminator.yaml", "f09.yaml", drops},
		{"f10-refs.yaml", "f10.yaml", interns},
		{"f11-refs.yaml", "f11.yaml", interns},
		{"f12-refs.yaml", "m.yaml", internsNoisy},
		{"f13-refs.yaml", "f13.yaml", drops},
		{"f28-maps-additional.yaml", "f28.yaml", interns},
		{"f30-protocol-surface.yaml", "f30.yaml", drops},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			doc, diags := compileFile(t, danglingDir, tc.file, tc.srcPath)
			assert.Empty(t, danglingRefs(doc), "IR must be referentially closed")
			switch tc.want {
			case drops:
				assert.True(t, hasErrorRef(diags),
					"a dropped entry must leave an error-severity unresolved-ref diagnostic")
			case interns:
				assert.False(t, hasAnyError(diags),
					"an interned reference must not raise an error diagnostic")
			case internsNoisy:
				// f12 resolves the same-file ref internally; the loader still reports
				// the external m.yaml it could not open. Only closure is asserted.
			}
		})
	}
}

// TestDanglingRefs_f07 pins the positive case: a mapping value that contains '/'
// but names an existing schema resolves to it rather than dangling.
func TestDanglingRefs_f07(t *testing.T) {
	t.Parallel()
	doc, diags := compileFile(t, danglingDir, "f07-discriminator.yaml", "f07.yaml")
	assert.False(t, hasAnyError(diags))
	pet, ok := doc.Types[namedID("Pet")].(*ir.Union)
	require.True(t, ok)
	require.NotNil(t, pet.Discriminator)
	target, ok := pet.Discriminator.Mapping["x"]
	require.True(t, ok, "the mapping entry survives")
	// The schema is literally named "A/B"; its pointer segment is RFC 6901-escaped.
	assert.Equal(t, ir.TypeID("t/openapi/components/schemas/A~1B"), target,
		"resolves to the existing schema named A/B")
	_, ok = doc.Types[target]
	assert.True(t, ok, "the resolved target is present in the registry")
}

// TestDanglingRefs_f30 pins the auth case: a security requirement naming an
// undeclared scheme is dropped and diagnosed, never written as a dangling AuthID.
func TestDanglingRefs_f30(t *testing.T) {
	t.Parallel()
	doc, diags := compileFile(t, danglingDir, "f30-protocol-surface.yaml", "f30.yaml")
	assert.Empty(t, danglingRefs(doc))
	assert.Empty(t, doc.Auth, "no scheme is declared")
	require.Len(t, doc.Services, 1)
	require.Len(t, doc.Services[0].Auth, 1, "the requirement option is kept")
	assert.Empty(t, doc.Services[0].Auth[0].Schemes, "its undeclared scheme is dropped")
	assert.True(t, hasErrorRef(diags), "the drop is diagnosed")
}

// TestDanglingRefs_f10 pins that a $ref to a component sub-schema interns the
// sub-schema at its pointer-derived ID so the reference resolves.
func TestDanglingRefs_f10(t *testing.T) {
	t.Parallel()
	doc, _ := compileFile(t, danglingDir, "f10-refs.yaml", "f10.yaml")
	foo, ok := doc.Types[namedID("Foo")].(*ir.Model)
	require.True(t, ok)
	x, ok := propByWire(foo, "x")
	require.True(t, ok)
	assert.Equal(t, ir.TypeID("t/anon/components/schemas/Foo/properties/bar"), x.Type.Target)
	_, ok = doc.Types[x.Type.Target]
	assert.True(t, ok, "the referenced sub-schema is interned under its pointer-derived ID")
}

// TestDanglingRefs_Corpus runs the oracle over the whole conformance corpus and
// the petstore golden so a future change cannot reintroduce a dangling reference.
func TestDanglingRefs_Corpus(t *testing.T) {
	t.Parallel()
	specs, err := filepath.Glob(filepath.Join(conformanceDir, "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, specs)
	specs = append(specs, "../../testdata/golden/openapi/petstore.yaml")
	for _, spec := range specs {
		t.Run(filepath.Base(spec), func(t *testing.T) {
			t.Parallel()
			base := filepath.Base(spec)
			doc, _ := compileFile(t, filepath.Dir(spec), base, base)
			assert.Empty(t, danglingRefs(doc), "corpus spec %s must be referentially closed", base)
		})
	}
}
