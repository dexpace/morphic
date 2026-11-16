// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package openapicases

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/internal/harness"
)

// groupAxis names the axis of the test suite's groups.
const groupAxis = "group"

// groupPath is the path a group's schema is held at, beside the root.
const groupPath = "g.json"

// dialect is the JSON Schema dialect a group's schema is read in, which the
// suite leaves the runner to say.
const dialect = "https://json-schema.org/draft/2020-12/schema"

// groupCases is the product of the test suite's groups holding a valid
// instance, each written as the default of a use of its schema: a product of
// its own, as no construct is written beside a group.
type groupCases struct {
	versions  []version
	groups    []Group
	positions []position
	spellings []string
	// suite is why the vendored test suite did not read, nil where it did.
	suite error
}

// Groups returns the OpenAPI compiler's cases of the test suite's groups, at
// each version reading JSON Schema 2020-12 and each position whose defaults
// the validate pass judges. A suite that does not read leaves every case to
// panic with why when written, so no run passes without it.
func Groups() harness.Cases {
	groups, err := SuiteGroups()
	groups = slices.DeleteFunc(groups, func(g Group) bool {
		return !slices.ContainsFunc(g.Tests, func(t Test) bool { return t.Valid })
	})
	return groupCases{
		versions:  slices.DeleteFunc(versions(), func(v version) bool { return !v.dialect }),
		groups:    groups,
		positions: slices.DeleteFunc(positions(), func(p position) bool { return p.carry == nil }),
		spellings: slices.DeleteFunc(spellings(), func(s string) bool { return s == inline }),
		suite:     err,
	}
}

// Axes names the axes: version, group, position and spelling. A group is
// named by its Name with "%" and the "/" no value holds percent-encoded.
func (c groupCases) Axes() []harness.Axis {
	axes := []harness.Axis{{Name: versionAxis}, {Name: groupAxis}, {Name: positionAxis}, {Name: spellingAxis}}
	for _, v := range c.versions {
		axes[0].Values = append(axes[0].Values, harness.Value{Name: v.name, Like: v.like})
	}
	escape := strings.NewReplacer("%", "%25", "/", "%2F")
	for _, g := range c.groups {
		axes[1].Values = append(axes[1].Values, harness.Value{Name: escape.Replace(g.Name)})
	}
	for _, p := range c.positions {
		axes[2].Values = append(axes[2].Values, harness.Value{Name: p.name})
	}
	axes[3].Values = names(c.spellings)
	return axes
}

// Write returns the case at: the group's schema as a source of its own, each
// schema the group serves held at its URI, and a carrier at the position for
// each valid instance, its use of the schema defaulting to the instance. It
// returns false for indices outside the product and for any spelling but
// inline, as Write of the schema cases does. A group the writer cannot
// encode, and a suite that did not read, panic, which CheckCase reports as the
// case's outcome.
func (c groupCases) Write(at []int) (harness.Document, bool) {
	if c.suite != nil {
		panic(c.suite.Error())
	}
	if len(at) != 4 || !within(at[0], c.versions) || !within(at[1], c.groups) || !within(at[2], c.positions) ||
		!within(at[3], c.spellings) {
		return harness.Document{}, false
	}
	v, g, p := c.versions[at[0]], &c.groups[at[1]], c.positions[at[2]]
	var uses []object
	var tests []Test
	for _, t := range g.Tests {
		if !t.Valid {
			continue
		}
		use, ok := spell(c.spellings[at[3]], object{{"$ref", groupPath}}, object{{"default", raw(string(t.Data))}})
		if !ok {
			return harness.Document{}, false
		}
		uses, tests = append(uses, use), append(tests, t)
	}
	paths, schemas, nodes := p.carry(uses)
	root, err := encode(object{
		{"openapi", v.name},
		{"info", object{{"title", "cases"}, {"version", "1"}}},
		{"paths", paths},
		{"components", object{{"schemas", schemas}}},
	})
	if err == nil {
		var schema []byte
		if schema, err = withDialect(g.Schema); err == nil {
			doc := groupDocument(root, schema, g, tests, nodes)
			doc.Equal = c.equal(at)
			return doc, true
		}
	}
	panic(fmt.Sprintf("openapicases: case %v: %v", at, err))
}

// within reports whether i indexes of.
func within[T any](i int, of []T) bool { return i >= 0 && i < len(of) }

// groupDocument returns the document of g's case: root, holding a carrier of
// each of tests at its node, and g's schema, held as schema.
func groupDocument(root, schema []byte, g *Group, tests []Test, nodes []string) harness.Document {
	doc := harness.Document{
		Sources: []compilers.Source{{Path: specPath, Data: root}, {Path: groupPath, Data: schema}},
		Options: map[string]string{"allow-external-refs": "true"},
	}
	for _, uri := range slices.Sorted(maps.Keys(g.External)) {
		doc.Sources = append(doc.Sources, compilers.Source{Path: uri, Data: g.External[uri]})
	}
	for i, t := range tests {
		doc.Instances = append(doc.Instances, harness.Instance{Node: nodes[i], Valid: true, Name: t.Description})
	}
	return doc
}

// withDialect returns schema with a "$schema" member naming dialect first,
// where schema is an object writing none. A schema that is no object, such as
// a boolean one, and one naming its own dialect are returned as they are.
func withDialect(schema jsontext.Value) ([]byte, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(schema))
	open, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	if open.Kind() != '{' {
		return schema, nil
	}
	members := object{{"$schema", dialect}}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, err
		}
		name := tok.String() // before the next read voids tok
		value, err := dec.ReadValue()
		if err != nil {
			return nil, err
		}
		if name == "$schema" {
			return schema, nil
		}
		members = append(members, member{name, raw(string(value))})
	}
	return encode(members)
}

// carryProperties writes each use as the property "t<i>" of the case's
// component.
func carryProperties(uses []object) (object, object, []string) {
	props := make(object, len(uses))
	nodes := make([]string, len(uses))
	for i, u := range uses {
		name := "t" + strconv.Itoa(i)
		props[i] = member{name, u}
		nodes[i] = "p/openapi/components/schemas/" + caseSchema + "/properties/" + name
	}
	return nil, object{{caseSchema, object{{"type", "object"}, {"properties", props}}}}, nodes
}

// carryParameters writes each use as the schema of the query parameter
// "q<i>" of the case's operation.
func carryParameters(uses []object) (object, object, []string) {
	params := make([]any, len(uses))
	nodes := make([]string, len(uses))
	for i, u := range uses {
		name := "q" + strconv.Itoa(i)
		params[i] = object{{"name", name}, {"in", "query"}, {"schema", u}}
		nodes[i] = "param/openapi/paths/" + pointerToken(casePath) + "/get/parameters/" + name + "/query"
	}
	paths, schemas := at(object{{"get", object{{"parameters", params}, {"responses", ok}}}})
	return paths, schemas, nodes
}

// carryHeaders writes each use as the schema of the header "H<i>" of the case's
// response.
func carryHeaders(uses []object) (object, object, []string) {
	headers := make(object, len(uses))
	nodes := make([]string, len(uses))
	for i, u := range uses {
		name := "H" + strconv.Itoa(i)
		headers[i] = member{name, object{{"schema", u}}}
		nodes[i] = "p/openapi/paths/" + pointerToken(casePath) + "/get/responses/200/headers/" + name
	}
	paths, schemas := at(response(member{"headers", headers}))
	return paths, schemas, nodes
}

// pointerToken returns s as one JSON Pointer reference token (RFC 6901).
func pointerToken(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

// equal is what the case at, a group's uses in one spelling, must equal: the
// same uses in its partner spelling, each relocated beneath the carriers its
// position writes, and nil for wrap, the spelling the others equal.
func (c groupCases) equal(at []int) *harness.Equal {
	partner, rule, ok := partnerOf(c.spellings[at[3]])
	if !ok {
		return nil
	}
	to := slices.Clone(at)
	to[3] = slices.Index(c.spellings, partner)
	return &harness.Equal{To: to, Rule: rulePrefix + rule, Relocate: relocation(carriersOf(c.positions[at[2]]))}
}

// carriersOf is the pointer every carrier p writes for a group lies beneath:
// its own schema's pointer less that schema and the carrier's name or index.
func carriersOf(p position) string {
	at := strings.TrimSuffix(p.pointer, "/schema")
	return at[:strings.LastIndex(at, "/")+1]
}
