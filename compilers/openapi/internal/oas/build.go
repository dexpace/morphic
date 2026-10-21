package oas

// The constructors the table files are written in. A row is a call chain
// read left to right: the field and its first citation, then what the
// specification adds about it.

// sectioned returns a constructor of citations under one heading.
func sectioned(section string) func(Minor, string) Cite {
	return func(m Minor, sentence string) Cite {
		return Cite{Minor: m, Section: section, Sentence: sentence}
	}
}

// field returns a field defined at every minor, optional, citing cites.
func field(name string, shape Shape, cites ...Cite) Field {
	return Field{Name: name, Shape: shape, Defined: Always(), Cites: cites}
}

// objectOf returns a field holding one object of kind k.
func objectOf(name string, k Kind, cites ...Cite) Field {
	f := field(name, Nested, cites...)
	f.Of = k
	return f
}

// mapOf returns a field holding a map of objects of kind k.
func mapOf(name string, k Kind, cites ...Cite) Field {
	f := field(name, Map, cites...)
	f.Of = k
	return f
}

// listOf returns a field holding a list of objects of kind k.
func listOf(name string, k Kind, cites ...Cite) Field {
	f := field(name, List, cites...)
	f.Of = k
	return f
}

// during narrows the minors that define the field.
func (f Field) during(s Span) Field {
	f.Defined = s
	return f
}

// required sets the minors at which the field must be present.
func (f Field) required(s Span) Field {
	f.Required = s
	return f
}

// nonEmpty sets the minors whose grammar refuses the empty string.
func (f Field) nonEmpty(s Span, cites ...Cite) Field {
	f.NonEmpty = s
	f.Cites = append(f.Cites, cites...)
	return f
}

// admits lists the values the field admits.
func (f Field) admits(values ...Value) Field {
	f.Values = values
	return f
}

// also adds a shape the field admits from the minors in s.
func (f Field) also(shape Shape, s Span) Field {
	f.Alts = append(f.Alts, Alt{Shape: shape, Span: s})
	return f
}

// formatted sets the syntax of the field's string value.
func (f Field) formatted(format Format) Field {
	f.Format = format
	return f
}

// keyed sets the syntax of the field's map keys.
func (f Field) keyed(format Format, cites ...Cite) Field {
	f.Keys = format
	f.Cites = append(f.Cites, cites...)
	return f
}

// ownedBy names the package that reports the field's missing-field finding.
func (f Field) ownedBy(pkg string) Field {
	f.Owner = pkg
	return f
}

// keyword marks the field a JSON Schema keyword.
func (f Field) keyword() Field {
	f.Keyword = true
	return f
}

// val returns a value every minor admits.
func val(name string) Value { return Value{Name: name, Span: Always()} }

// vals returns the values every minor admits.
func vals(names ...string) []Value {
	out := make([]Value, len(names))
	for i, n := range names {
		out[i] = val(n)
	}
	return out
}

// valFrom returns a value admitted from minor m.
func valFrom(name string, m Minor) Value { return Value{Name: name, Span: From(m)} }

// valWhen returns a value admitted when the terms hold.
func valWhen(name string, s Span, cond ...Term) Value {
	return Value{Name: name, Span: s, When: cond}
}

// term returns the condition that sibling field name holds one of in.
func term(name string, in ...string) Term { return Term{Field: name, In: in} }

// pattern returns a family of keys every minor admits.
func pattern(name string, shape Shape, k Kind, cites ...Cite) Pattern {
	return Pattern{Name: name, Shape: shape, Of: k, Span: Always(), Cites: cites}
}

// exclusive returns a rule: at most one of fields.
func exclusive(s Span, fields []string, cites ...Cite) Rule {
	return Rule{Kind: RuleExclusive, Span: s, Fields: fields, Cites: cites}
}

// exactlyOne returns a rule: one of fields, and only one.
func exactlyOne(s Span, fields []string, cites ...Cite) Rule {
	return Rule{Kind: RuleExactlyOne, Span: s, Fields: fields, Cites: cites}
}

// anyOf returns a rule: at least one of fields.
func anyOf(s Span, fields []string, cites ...Cite) Rule {
	return Rule{Kind: RuleAnyOf, Span: s, Fields: fields, Cites: cites}
}

// count returns a rule: field holds at least one entry and at most hi; zero
// is no cap.
func count(s Span, lv Level, name string, hi int, cites ...Cite) Rule {
	return Rule{Kind: RuleCount, Span: s, Level: lv, Field: name, Min: 1, Max: hi, Cites: cites}
}

// requiredWhen returns a rule: name is present when the terms hold.
func requiredWhen(s Span, name string, cond []Term, cites ...Cite) Rule {
	return Rule{Kind: RuleRequiredWhen, Span: s, Field: name, When: cond, Cites: cites}
}

// forbiddenWhen returns a rule: none of fields is present when the terms hold.
func forbiddenWhen(s Span, fields []string, cond []Term, cites ...Cite) Rule {
	return Rule{Kind: RuleForbiddenWhen, Span: s, Fields: fields, When: cond, Cites: cites}
}

// mustBe returns a rule: name is value when the terms hold.
func mustBe(s Span, name, value string, cond []Term, cites ...Cite) Rule {
	return Rule{Kind: RuleMustBe, Span: s, Field: name, Value: value, When: cond, Cites: cites}
}

// member returns a rule: name's value is among the strings of list.
func member(s Span, lv Level, name, list string, cites ...Cite) Rule {
	return Rule{Kind: RuleMember, Span: s, Level: lv, Field: name, Of: list, Cites: cites}
}

// uniqueNames returns a rule: the items of list have names that differ.
func uniqueNames(s Span, list string, cites ...Cite) Rule {
	return Rule{Kind: RuleUnique, Span: s, Field: list, Key: "name", Cites: cites}
}

// inapplicable returns a rule: name has no effect when the terms hold.
func inapplicable(s Span, name string, cond []Term, cites ...Cite) Rule {
	return Rule{Kind: RuleInapplicable, Span: s, Level: Should, Field: name, When: cond, Cites: cites}
}

// code returns a rule written in Go, reported by owner when it is not empty.
func code(s Span, name, owner string, cites ...Cite) Rule {
	return Rule{Kind: RuleCode, Span: s, Name: name, Owner: owner, Cites: cites}
}

// softened grades rules down to SHOULD: they rest on a sentence's wording by
// analogy, not on a sentence of their own, so a finding is a warning.
func softened(rules []Rule) []Rule {
	for i := range rules {
		rules[i].Level = Should
	}
	return rules
}

// owned marks a rule another package already reports.
func (r Rule) owned(pkg string) Rule {
	r.Owner = pkg
	return r
}

// fields lists names.
func fields(names ...string) []string { return names }

// when lists terms.
func when(terms ...Term) []Term { return terms }

// The packages that report a rule the table also states.
const (
	ownerOperation = "operation"
	ownerLoad      = "load"
	ownerAuth      = "auth"
)
