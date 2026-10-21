package oas

import "slices"

// Kind names an OpenAPI object. The zero value is no object.
type Kind uint8

// The objects of the specification. Raw values (an example, a default) are
// not objects: a field holding one has the Any shape.
const (
	Document Kind = 1 + iota
	Info
	Contact
	License
	Server
	ServerVariable
	Components
	Paths
	PathItem
	Operation
	ExternalDocs
	Parameter
	RequestBody
	MediaType
	Encoding
	Responses
	Response
	Callback
	Example
	Link
	Header
	Tag
	Reference
	Schema
	Discriminator
	XML
	SecurityScheme
	OAuthFlows
	OAuthFlow
	SecurityRequirement
	kindEnd
)

// Kinds lists every object kind in declaration order.
func Kinds() []Kind {
	out := make([]Kind, 0, kindEnd-1)
	for k := Document; k < kindEnd; k++ {
		out = append(out, k)
	}
	return out
}

// Shape is the form a field's value takes in the source.
type Shape uint8

// The shapes a value takes. Any, the zero value, leaves a value unchecked.
const (
	Any Shape = iota
	String
	Boolean
	Number
	// PositiveNumber is a number strictly greater than zero.
	PositiveNumber
	// Natural is a non-negative integer.
	Natural
	// Nested is a mapping read as an object of the Of kind.
	Nested
	// Map is a mapping whose every entry is read as the Of kind.
	Map
	// List is a sequence whose every item is read as the Of kind.
	List
	// StringMap is a mapping whose entries are strings.
	StringMap
	// StringList is a sequence of strings.
	StringList
	// RawMap is a mapping whose entries are kept unread.
	RawMap
	// RawList is a sequence whose items are kept unread.
	RawList
)

// Format is a syntax a string value, or a map key, must have.
type Format uint8

// The syntaxes the table names. FormatNone, the zero value, is no syntax.
const (
	FormatNone Format = iota
	// URI is a URI reference: relative references are admitted.
	URI
	// AbsoluteURI is a URI with a scheme.
	AbsoluteURI
	// Email is an email address.
	Email
	// ComponentName is a Components Object key: ^[a-zA-Z0-9\.\-_]+$.
	ComponentName
	// Token is a name of one or more token characters: an HTTP field name, a
	// cookie name (RFC 9110 §5.6.2).
	Token
	// AuthScheme is a Token, with the empty string left to the lowering, which
	// reports a security scheme that names no HTTP scheme.
	AuthScheme
	// RuntimeExpression is a string whose braces hold runtime expressions, or
	// a runtime expression bare.
	RuntimeExpression
	// VersionNumber is a version of the specification: major.minor.patch,
	// each a run of decimal digits.
	VersionNumber
	// ServerURL is a URL reference that may write {variables}.
	ServerURL
	// MediaRanges is a comma-separated list of media types, each specific or
	// a wildcard range.
	MediaRanges
	// AnchorName is a name a $anchor or a $dynamicAnchor declares: a letter or
	// '_', then letters, digits, '-', '_' and '.' (JSON Schema 2020-12 §8.2.2).
	AnchorName
)

// Level is how firmly the specification states a rule.
type Level uint8

// The levels. A finding of a Must rule is an error and of a Should rule a
// warning, matching RFC 2119.
const (
	Must Level = iota
	Should
)

// Cite is the sentence of a specification text a row comes from. Sentence is
// a verbatim fragment of the text of Minor, under the heading Section.
type Cite struct {
	Minor    Minor
	Section  string
	Sentence string
}

// Term is one condition on the object a rule judges: the sibling field Field
// holds one of In. The field "$key" is the key the parent holds the object
// under.
type Term struct {
	Field string
	In    []string
}

// Value is one value a field admits, from the minors in Span, and only where
// every When term holds.
type Value struct {
	Name string
	Span Span
	When []Term
}

// Alt is a second shape a field admits, from the minors in Span.
type Alt struct {
	Shape Shape
	Span  Span
}

// Field is one field of an object.
type Field struct {
	Name  string
	Shape Shape
	// Of is the object kind an Object, Map or List holds.
	Of Kind
	// Defined is the minors whose object defines the field. A construct
	// written at an earlier minor is reported as added by a later one.
	Defined Span
	// Required is the minors at which the field must be present; empty
	// means never.
	Required Span
	// NonEmpty is the minors whose grammar refuses the empty string.
	NonEmpty Span
	// Values is the values admitted, if the field is enumerated.
	Values []Value
	// Alts are the other shapes the field admits.
	Alts []Alt
	// Format is the syntax of a string value; Keys that of a map's keys.
	Format Format
	Keys   Format
	// Owner is the package that reports the field's missing-field finding.
	Owner string
	// ShapeOwner is the package that reports a value of the wrong shape.
	ShapeOwner string
	// Keyword marks a JSON Schema keyword: one a later minor adds is read
	// as the schema lowering reads it, not reported here.
	Keyword bool
	Cites   []Cite
}

// Pattern is a family of keys an object admits beside its fixed fields:
// every key that is neither one of them nor an extension.
type Pattern struct {
	// Name spells the key as the specification does: "/{path}".
	Name  string
	Shape Shape
	Of    Kind
	Span  Span
	// Keys is the syntax of the key itself.
	Keys  Format
	Cites []Cite
}

// RuleKind names what a Rule demands.
type RuleKind uint8

// The rule kinds. Each rule reads the fields it names on one object.
const (
	// RuleExclusive: at most one of Fields is present.
	RuleExclusive RuleKind = 1 + iota
	// RuleExactlyOne: exactly one of Fields is present.
	RuleExactlyOne
	// RuleAnyOf: at least one of Fields is present.
	RuleAnyOf
	// RuleCount: Field holds between Min and Max entries; Max 0 is no cap.
	RuleCount
	// RuleRequiredWhen: Field is present when every When term holds.
	RuleRequiredWhen
	// RuleForbiddenWhen: no field of Fields is present when every When
	// term holds.
	RuleForbiddenWhen
	// RuleMustBe: Field, when written, is Value if every When term holds.
	RuleMustBe
	// RuleMember: Field's value is among the strings of the list Of.
	RuleMember
	// RuleUnique: the items of the list Field are unique by their Key.
	RuleUnique
	// RuleSyntax: Field, when written, has the syntax Format if every When
	// term holds.
	RuleSyntax
	// RuleInapplicable: Field has no effect when every When term holds.
	RuleInapplicable
	// RuleMisplaced: no field of Fields is present when every When term
	// holds; one that is lowers as declared.
	RuleMisplaced
	// RuleCode: a predicate written in Go, named by Name.
	RuleCode
)

// Rule binds several fields of an object, or judges one beyond its shape.
type Rule struct {
	Kind  RuleKind
	Span  Span
	Level Level
	// Fields is the fields an exclusion, alternative or ban names.
	Fields []string
	// Field is the field a conditional, count, member or unique rule judges.
	Field string
	// Of names the list field a Member rule reads.
	Of string
	// Key is the entry key a Unique rule compares.
	Key string
	// Value is the value a MustBe rule demands.
	Value string
	// Format is the syntax a Syntax rule demands of Field.
	Format Format
	// Min and Max bound a Count rule.
	Min, Max int
	When     []Term
	// Scope says, for an Inapplicable rule, where the field does apply.
	Scope string
	// Name names the predicate of a Code rule.
	Name string
	// Owner is the package that already reports the rule; the engine skips it.
	Owner string
	Cites []Cite
}

// Object is one OpenAPI object: its fields, its pattern fields and its rules.
type Object struct {
	Kind Kind
	// Name is the specification's title for the object: "Info Object".
	Name       string
	Fields     []Field
	Patterned  []Pattern
	Rules      []Rule
	Extensible bool
	// Referenceable is the minors at which a Reference Object may stand in
	// the object's place.
	Referenceable Span
	// Boolean is the minors at which true or false may stand in the
	// object's place.
	Boolean Span
}

// Spec is the table: every object of the specification.
type Spec struct {
	objects []Object
	index   map[Kind]int
}

// Objects returns the table's objects in declaration order.
func (s Spec) Objects() []Object { return append([]Object(nil), s.objects...) }

// Object returns the object of kind k.
func (s Spec) Object(k Kind) (Object, bool) {
	i, ok := s.index[k]
	if !ok {
		return Object{}, false
	}
	return s.objects[i], true
}

// must returns the object of kind k, which the table holds for every kind.
func (s Spec) must(k Kind) Object { return s.objects[s.index[k]] }

// Table returns the specification's rules for OpenAPI 3.0, 3.1 and 3.2.
func Table() Spec {
	objs := slices.Concat(documentObjects(), pathObjects(), parameterObjects(), responseObjects(),
		componentObjects(), securityObjects(), schemaObjects())
	index := make(map[Kind]int, len(objs))
	for i, o := range objs {
		index[o.Kind] = i
	}
	return Spec{objects: objs, index: index}
}
