// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package oas

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/nodeview"
	"github.com/dexpace/morphic/ir"
)

// The classes a finding is reported under, each appended to diag.Validation.
// The first group names the rules the parser library reports under the same
// words.
const (
	classRequired  = "validation-required-field"
	classType      = "validation-type-mismatch"
	classValues    = "validation-allowed-values"
	classFormat    = "validation-invalid-format"
	classSyntax    = "validation-invalid-syntax"
	classExclusive = "validation-mutually-exclusive-fields"
	classEmpty     = "validation-empty-value"
	classDuplicate = "validation-duplicate-key"
	classReference = "validation-invalid-reference"
	classCircular  = "validation-circular-reference"
	// classCount: a map or list holds too few or too many entries.
	classCount = "validation-entry-count"
	// classForbidden: a field is written where the specification bars it.
	classForbidden = "validation-forbidden-field"
	// classNoEffect: a field is written where the specification gives it no
	// effect.
	classNoEffect = "validation-no-effect"
)

// reporter collects the findings of one check.
type reporter struct {
	locate   Locator
	declared string
	diags    []ir.Diagnostic
	// readAt says whether a finding placed elsewhere than it was read names
	// where it was read (Options.ReadAt), under rootAt (Options.Root).
	readAt bool
	rootAt jsontext.Pointer
	// said holds each finding made, as placed, before readAt adds to it.
	said map[ir.Diagnostic]bool
}

// report adds a finding of class at ptr.
func (r *reporter) report(sev ir.Severity, class string, ptr jsontext.Pointer, format string, args ...any) {
	r.add(diag.Newf(sev, diag.Validation+"/"+class, r.locate(ptr), format, args...), ptr)
}

// add adds d, a finding about the node read at ptr, unless it was made already:
// a node an alias mounts at two positions is read at each, and a defect in it
// is one defect, at one node. Where readAt holds and that node is written
// elsewhere, the message names the position it was first read at.
func (r *reporter) add(d ir.Diagnostic, ptr jsontext.Pointer) {
	if r.said[d] {
		return
	}
	r.said[d] = true
	if read := r.rootAt + ptr; r.readAt && d.Provenance.Pointer != "" && d.Provenance.Pointer != read {
		d.Message += readAtClause + string(read)
	}
	r.diags = append(r.diags, d)
}

// readAtClause begins what Options.ReadAt adds to a message.
const readAtClause = "; read at "

// AsPlaced returns d without the position Options.ReadAt names: the finding as
// it is placed, which is the same whichever position read the node it is about.
func AsPlaced(d ir.Diagnostic) ir.Diagnostic {
	if i := strings.LastIndex(d.Message, readAtClause); i >= 0 {
		d.Message = d.Message[:i]
	}
	return d
}

// laterField adds the warning for a construct a later minor than the declared
// one added to obj: the field key, or the value of it. since is the minor that
// added it.
func (r *reporter) laterField(obj Object, key, value string, since Minor, ptr jsontext.Pointer) {
	what := fmt.Sprintf("field %q", key)
	if value != "" {
		what = fmt.Sprintf("value %q of field %q", value, key)
	}
	r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"%s of the %s was added in OpenAPI %s, and this document declares %s; it is lowered as in an OpenAPI %s document",
		what, obj.Name, since, r.declared, since), ptr)
}

// laterKeyword adds the warning for key, a keyword the dialect the compiler
// reads natively defines and rows, a Schema Object's, do not: at 3.0, as
// laterField words a construct 3.1 added, bar a `$schema`, which chooses no
// dialect there (docs/compilers/openapi.md#dialect-keywords); in a JSON Schema
// draft, as a keyword of 2020-12, bar a `prefixItems` beside the draft's own
// tuple, which tuple says the schema writes, and which is kept verbatim.
func (r *reporter) laterKeyword(rows SchemaRules, key string, tuple bool, ptr jsontext.Pointer) {
	if !rows.rows.draft {
		if key != "$schema" {
			r.laterField(rows.rows.object, key, "", V31, ptr)
			return
		}
		r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
			"field %q of the %s was added in OpenAPI %s, and this document declares %s; a 3.0 schema is read in "+
				"3.0's own dialect whatever it names, so it is kept verbatim and chooses nothing",
			key, rows.rows.object.Name, V31, r.declared), ptr)
		return
	}
	beside := ""
	if key == "prefixItems" && tuple {
		beside = "; this schema writes its dialect's own tuple, an items list, so prefixItems is kept verbatim beside it"
	}
	r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"keyword %q is not defined by this schema's dialect; it is read as JSON Schema 2020-12 defines it%s",
		key, beside), ptr)
}

// ignoredBesideRef adds the warning for key, written beside a $ref in a
// schema whose dialect ignores the keywords there, as the JSON Schema drafts
// before 2019-09 do, and applied as 2020-12 applies it, the reading that loses
// least (docs/compilers/openapi.md#later-version-construct).
func (r *reporter) ignoredBesideRef(key string, ptr jsontext.Pointer) {
	r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"keyword %q beside a $ref is ignored by this schema's dialect, as by the JSON Schema drafts "+
			"before 2019-09; it is applied as in 2020-12", key), ptr)
}

// readsBesideRef reports whether applying key, written beside a $ref in a
// schema read with rules, reads otherwise than ignoring it: a field the rows
// define, OpenAPI's own among them, bar the $ref, the `$schema` naming the
// dialect, `definitions`, whose schemas only a $ref reads, `$comment`, which
// nothing reads, and the identifier, which the binder ignores there as the
// dialect does (bind.DialectRule.IgnoresRefFields). A key they define none for,
// an extension or an unknown keyword, is kept as written either way.
func readsBesideRef(rules SchemaRules, key string) bool {
	switch key {
	case "$ref", "$schema", "definitions", "$comment", rules.Identifier():
		return false
	default:
		return rules.Defines(key)
	}
}

// laterSibling adds the warning for key, written beside the $ref of obj in a
// document declaring minor, which ignores it, where since applies it: it is
// applied as since applies it and read as minor defines it.
func (r *reporter) laterSibling(obj Object, key string, minor, since Minor, ptr jsontext.Pointer) {
	r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"keyword %q beside the $ref of a %s is ignored by OpenAPI %s and applied by OpenAPI %s, "+
			"and this document declares %s; it is applied as in an OpenAPI %s document "+
			"and read as in an OpenAPI %s one",
		key, obj.Name, minor, since, r.declared, since, minor), ptr)
}

// repeated warns of each key the mapping at ptr writes more than once: the
// model reads the last copy and drops the rest without a word. what names the
// mapping in the message.
func (w *walker) repeated(node *yaml.Node, ptr jsontext.Pointer, what string) {
	for _, key := range nodeview.RepeatedKeys(node) {
		w.report(ir.SeverityWarning, classDuplicate, ptr.AppendToken(key),
			"%s writes the key %q more than once, and only the last copy is read in OpenAPI %s (Format)", what, key, w.minor)
	}
}

// severity is the severity of a finding of a rule stated at the level.
func severity(lv Level) ir.Severity {
	if lv == Should {
		return ir.SeverityWarning
	}
	return ir.SeverityError
}

// describeFormat says what a string with the syntax is.
func describeFormat(format Format) string {
	return map[Format]string{
		URI: "a URI reference", AbsoluteURI: "a URI with a scheme", Email: "an email address",
		ComponentName:     "a name of letters, digits, '.', '-' and '_'",
		Token:             "a token of letters, digits and !#$%&'*+-.^_`|~",
		AuthScheme:        "a token of letters, digits and !#$%&'*+-.^_`|~",
		RuntimeExpression: "a runtime expression, or text whose braces hold runtime expressions",
		VersionNumber:     "a version number of the form major.minor.patch",
		ServerURL:         "a URL reference, its variables aside",
		MediaRanges:       "a comma-separated list of media types and media type ranges",
		AnchorName:        "a name of a letter or '_', then letters, digits, '-', '_' and '.'",
		PlainName:         "a name of a letter, then letters, digits, '-', '_', ':' and '.'",
		PlainNameURI:      "a URI reference whose fragment, where written alone and no JSON pointer, is a name of a letter, then letters, digits, '-', '_', ':' and '.'",
		ResourceURI:       "a URI reference whose fragment, if any, is empty",
	}[format]
}

// describeTerms spells a condition the way the document writes it: in=header.
func describeTerms(terms []Term) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = describeTerm(t.Field, strings.Join(t.In, "|"))
	}
	return strings.Join(parts, ", ")
}

// describeTerm spells one term holding text: a field and its value, in=header,
// or, for "$key", which no field names, the key the object is held under.
func describeTerm(field, text string) string {
	if field == "$key" {
		return "held under " + text
	}
	return field + "=" + text
}
