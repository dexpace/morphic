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

// later adds the warning for a construct a later minor than the declared one
// added to obj: the field key, or the value of it. since is the minor that
// added it.
func (r *reporter) later(obj Object, key, value string, since Minor, ptr jsontext.Pointer) {
	what := fmt.Sprintf("field %q", key)
	if value != "" {
		what = fmt.Sprintf("value %q of field %q", value, key)
	}
	r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"%s of the %s was added in OpenAPI %s, and this document declares %s; it is lowered as in an OpenAPI %s document",
		what, obj.Name, since, r.declared, since), ptr)
}

// laterSibling adds the warning for key, written beside the $ref of obj in a
// document declaring minor, which ignores it, where since applies it.
func (r *reporter) laterSibling(obj Object, key string, minor, since Minor, ptr jsontext.Pointer) {
	r.add(diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"keyword %q beside the $ref of a %s is ignored by OpenAPI %s and applied by OpenAPI %s, "+
			"and this document declares %s; it is lowered as in an OpenAPI %s document",
		key, obj.Name, minor, since, r.declared, since), ptr)
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
