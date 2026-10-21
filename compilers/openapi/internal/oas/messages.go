package oas

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
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
	// classCount: a map or list holds too few or too many entries.
	classCount = "validation-entry-count"
	// classForbidden: a field is written where the specification bars it.
	classForbidden = "validation-forbidden-field"
)

// reporter collects the findings of one check.
type reporter struct {
	locate   Locator
	declared string
	diags    []ir.Diagnostic
}

// report adds a finding of class at ptr.
func (r *reporter) report(sev ir.Severity, class string, ptr jsontext.Pointer, format string, args ...any) {
	r.diags = append(r.diags, diag.Newf(sev, diag.Validation+"/"+class, r.locate(ptr), format, args...))
}

// later adds the warning for a construct a later minor than the declared one
// added to obj: the field key, or the value of it. since is the minor that
// added it.
func (r *reporter) later(obj Object, key, value string, since Minor, ptr jsontext.Pointer) {
	what := fmt.Sprintf("field %q", key)
	if value != "" {
		what = fmt.Sprintf("value %q of field %q", value, key)
	}
	r.diags = append(r.diags, diag.Newf(ir.SeverityWarning, diag.LaterVersionConstruct, r.locate(ptr),
		"%s of the %s was added in OpenAPI %s, and this document declares %s; it is lowered as in an OpenAPI %s document",
		what, obj.Name, since, r.declared, since))
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
		ComponentName: "a name of letters, digits, '.', '-' and '_'",
	}[format]
}

// describeTerms spells a condition the way the document writes it: in=header.
func describeTerms(terms []Term) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = t.Field + "=" + strings.Join(t.In, "|")
	}
	return strings.Join(parts, ", ")
}
