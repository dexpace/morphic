// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package specrules_test

import (
	"testing"

	"github.com/dexpace/morphic/compilers/openapi/internal/specrules"
)

// TestCheck_ContentKeyIsAMediaType holds a content map's keys, at each of the
// four objects that write one, to a media type or a media type range: the
// type/subtype form with parameters, */* and type/*. A key that is neither is
// a syntax warning at the key.
func TestCheck_ContentKeyIsAMediaType(t *testing.T) {
	body := func(key string) string { return `{content: {` + key + `: {schema: {type: string}}}}` }
	response := func(key string) string { return `{description: ok, content: {` + key + `: {}}}` }
	parameter := func(key string) string {
		return `{name: q, in: query, content: {` + key + `: {schema: {type: string}}}}`
	}
	header := func(key string) string { return `{content: {` + key + `: {schema: {type: string}}}}` }
	verify(t, []scenario{
		{"a media type", "3.1.0", specrules.RequestBody, body(`application/json`), nil},
		{"a range", "3.1.0", specrules.RequestBody, body(`"text/*"`), nil},
		{"any type", "3.1.0", specrules.RequestBody, body(`"*/*"`), nil},
		{"a media type with a parameter", "3.1.0", specrules.RequestBody, body(`"application/json; charset=utf-8"`), nil},
		{"a space in the type", "3.1.0", specrules.RequestBody, body(`"application json"`), []finding{at(badSyntax, "/content/application json")}},
		{"no slash", "3.0.3", specrules.RequestBody, body(`json`), []finding{at(badSyntax, "/content/json")}},
		{"no subtype", "3.2.0", specrules.RequestBody, body(`"application/"`), []finding{at(badSyntax, "/content/application~1")}},
		{"a response's bad key", "3.1.0", specrules.Response, response(`"application json"`), []finding{at(badSyntax, "/content/application json")}},
		{"a response's range", "3.1.0", specrules.Response, response(`"image/*"`), nil},
		{"a parameter's bad key", "3.1.0", specrules.Parameter, parameter(`"application json"`), []finding{at(badSyntax, "/content/application json")}},
		{"a parameter's media type", "3.1.0", specrules.Parameter, parameter(`application/json`), nil},
		{"a header's bad key", "3.1.0", specrules.Header, header(`"application json"`), []finding{at(badSyntax, "/content/application json")}},
		{"a header's media type", "3.1.0", specrules.Header, header(`text/plain`), nil},
	})
}

// TestCheck_SerializationKeywordsBesideContent holds a parameter or header
// serialized through its content to no style, explode or allowReserved of its
// own: each one written beside content is a no-effect warning at the key, and
// the same keys beside a schema, or content alone, draw none.
func TestCheck_SerializationKeywordsBesideContent(t *testing.T) {
	const content = `content: {application/json: {schema: {type: object}}}`
	verify(t, []scenario{
		{"the three keys beside content", "3.1.0", specrules.Parameter,
			`{name: q, in: query, style: form, explode: false, allowReserved: true, ` + content + `}`,
			[]finding{at(noEffect, "/style"), at(noEffect, "/explode"), at(noEffect, "/allowReserved")}},
		{"content alone", "3.1.0", specrules.Parameter, `{name: q, in: query, ` + content + `}`, nil},
		{"the keys beside a schema", "3.1.0", specrules.Parameter,
			`{name: s, in: query, style: form, explode: false, allowReserved: true, schema: {type: string}}`, nil},
		{"one key beside content", "3.0.3", specrules.Parameter,
			`{name: q, in: query, explode: true, ` + content + `}`, []finding{at(noEffect, "/explode")}},
		{"a location that also bars the key", "3.1.0", specrules.Parameter,
			`{name: q, in: path, required: true, allowReserved: true, ` + content + `}`,
			[]finding{at(noEffect, "/allowReserved"), at(noEffect, "/allowReserved")}},
		{"a header's keys beside content", "3.1.0", specrules.Header,
			`{style: simple, explode: true, ` + content + `}`, []finding{at(noEffect, "/style"), at(noEffect, "/explode")}},
		{"a header's keys beside a schema", "3.1.0", specrules.Header,
			`{style: simple, explode: true, schema: {type: string}}`, nil},
	})
}
