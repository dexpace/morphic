// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package statement

import (
	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/bind"
)

// The slots' values, one struct per slot and one field per keyword, each
// typed as the parser reads the keyword: a count as *int64, a schema as a Sub,
// and an instance as its node. A numeric bound, and a keyword the parser's
// model does not hold, is the node it was written as, nil for a null, since
// the lowering reads a bound as the decimal its text spells
// (oas.Field.Decimal).

// Kinds is what `type` and `nullable` say.
type Kinds struct {
	Types    []Kind
	Nullable *bool
}

// Kind is a name `type` writes: one of the seven JSON Schema names, or any
// other string it holds.
type Kind string

// The kinds JSON Schema names.
const (
	KindArray   Kind = "array"
	KindBoolean Kind = "boolean"
	KindInteger Kind = "integer"
	KindNull    Kind = "null"
	KindNumber  Kind = "number"
	KindObject  Kind = "object"
	KindString  Kind = "string"
)

// Values is what `enum` and `const` say.
type Values struct {
	Enum  []*yaml.Node
	Const *yaml.Node
}

// Format is the format a schema names, whose kind the name decides.
type Format struct {
	Name string
}

// Number is what the keywords asserting about a number say.
type Number struct {
	Minimum, Maximum                   *yaml.Node
	ExclusiveMinimum, ExclusiveMaximum *yaml.Node
	MultipleOf                         *yaml.Node
}

// Text is what the keywords asserting about a string say.
type Text struct {
	MinLength, MaxLength              *int64
	Pattern                           string
	ContentEncoding, ContentMediaType string
	ContentSchema                     Sub
}

// Array is what the keywords asserting about an array say. AdditionalItems,
// which only drafts before 2020-12 define, is its node.
type Array struct {
	Items, Contains, UnevaluatedItems Sub
	PrefixItems                       []Sub
	AdditionalItems                   *yaml.Node
	MinItems, MaxItems                *int64
	MinContains, MaxContains          *int64
	UniqueItems                       *bool
}

// Object is what the keywords asserting about an object say.
// DependentRequired, which the parser's model does not hold, is its node.
type Object struct {
	Properties, PatternProperties                              []Sub
	AdditionalProperties, PropertyNames, UnevaluatedProperties Sub
	Required                                                   []string
	DependentRequired                                          *yaml.Node
	MinProperties, MaxProperties                               *int64
	Discriminator                                              *Discriminator
}

// Discriminator is what a schema's discriminator says: the property, its
// mapping in source order, its default mapping, the node it was read from and
// where (the zero position where the bindings do not know the schema), and the
// keys the parser's model holds no field for (Schema.Unknown).
type Discriminator struct {
	PropertyName   string
	Mapping        []MappingEntry
	DefaultMapping string
	Node           *yaml.Node
	At             bind.Position
	Unknown        []string
}

// MappingEntry is one entry of a discriminator's mapping: the property value
// and the schema name or reference it maps to.
type MappingEntry struct {
	Value, Target string
}

// Choice is what `oneOf` and `anyOf` say.
type Choice struct {
	OneOf, AnyOf []Sub
}

// Logic is what the keywords no kind or conjunction holds say: the in-place
// applicators `not`, `if`, `then`, `else` and `dependentSchemas`, and the
// drafts' `dependencies`, which is its node.
type Logic struct {
	Not, If, Then, Else Sub
	DependentSchemas    []Sub
	Dependencies        *yaml.Node
}

// Notes is what a schema says of itself rather than of its values.
// `$comment`, which the parser's model does not hold, is its node.
type Notes struct {
	Title, Description   string
	Default, Example     *yaml.Node
	Examples             []*yaml.Node
	Deprecated, ReadOnly *bool
	WriteOnly            *bool
	XML                  *XML
	ExternalDocs         *ExternalDocs
	Comment              *yaml.Node
}

// XML is what a schema's xml object says, the node it was read from, and the
// keys the parser's model holds no field for (Schema.Unknown).
type XML struct {
	Name, Namespace, Prefix *string
	Attribute, Wrapped      *bool
	Node                    *yaml.Node
	Unknown                 []string
}

// ExternalDocs is what a schema's externalDocs object says, the node it was
// read from, and the keys the parser's model holds no field for
// (Schema.Unknown).
type ExternalDocs struct {
	Description, URL string
	Node             *yaml.Node
	Unknown          []string
}
