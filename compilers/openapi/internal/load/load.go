// Package load turns one source document into a parsed, reference-resolved
// OpenAPI document plus the identity metadata the rest of the compiler stamps
// into the IR.
//
// It sits on the entry side of the pipeline: nothing below it in the compiler
// calls back into it, and it knows nothing about lowering. Spec problems leave
// as ir.Diagnostic values; the Go error return is reserved for I/O and
// programmer errors.
package load

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"

	oas3 "github.com/speakeasy-api/openapi/jsonschema/oas3"
	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"
	yaml "gopkg.in/yaml.v3"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi/internal/diag"
	"github.com/dexpace/morphic/compilers/openapi/internal/scan"
	"github.com/dexpace/morphic/compilers/openapi/internal/value"
	"github.com/dexpace/morphic/ir"
)

// Options is what Load needs from the compiler's own options. It is a separate
// type rather than the compiler's, because openapi.Options is public API whose
// shape is fixed by ir-design §10 and most of it describes lowering, which
// nothing here can see.
type Options struct {
	// DisableExternalRefs stops reference resolution reaching outside the
	// document — off the filesystem or over the network.
	DisableExternalRefs bool
}

// errParse marks a hard failure to parse a source document — an I/O- or
// programmer-level error, distinct from a spec problem reported as a diagnostic.
var errParse = errors.New("parse source")

// maxSchemaScanDepth bounds the scalar scan of a schema node (styleguide
// bounded-recursion rule); a schema nested deeper is pathological, not a spec the
// compiler is expected to classify.
const maxSchemaScanDepth = 512

// Document is the successful output of the load phase: a parsed, resolved
// speakeasy document plus the identity metadata the rest of the compiler needs.
// A nil *Document with error-severity diagnostics means the source is a spec
// problem the compiler refuses to lower (e.g. an unsupported version). The
// normalized "openapi" + major.minor format reaches the IR through
// Source.Format alone; Document does not separately carry a
// compilers.SourceFormat, since nothing downstream ever read one.
type Document struct {
	Doc    *soa.OpenAPI  // parsed, reference-resolved document
	Source ir.SourceInfo // format tag, path, content hash
}

// Load parses, validates, and resolves one source document. Spec problems
// become ir.Diagnostic values; the Go error return is reserved for I/O and
// programmer errors (a hard unmarshal failure). A nil document with diagnostics
// signals a refusal to lower (unsupported version) without aborting the batch.
//
//nolint:unparam // srcIndex varies once Compile drives the multi-source loop
func Load(ctx context.Context, srcIndex int, src compilers.Source, opts Options) (*Document, []ir.Diagnostic, error) {
	cyc := scan.Cycles(srcIndex, src.Data)
	if diag.HasError(cyc) {
		return nil, cyc, nil // degenerate cycle: refuse to lower, do not crash the parser
	}
	// cyc may still hold a non-fatal scan-incomplete warning; carry it forward.

	doc, valErrs, err := unmarshal(ctx, src.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("openapi: unmarshal source %d: %w", srcIndex, err)
	}

	minor, ok := SupportedMinor(doc.OpenAPI)
	if !ok {
		return nil, append(cyc, diag.Newf(ir.SeverityError, diag.UnsupportedVersion,
			ir.Provenance{Source: srcIndex},
			"unsupported OpenAPI version %q; want 3.0, 3.1, or 3.2", doc.OpenAPI)), nil
	}

	diags := cyc
	wrongMetaSchema := metaSchemaVersionArtifacts(ctx, doc, minor)
	for _, ve := range valErrs {
		if verr, ok := asValidationError(ve); ok &&
			(numericLiteralArtifact(verr) || wrongMetaSchema[findingSite(verr)]) {
			continue
		}
		diags = append(diags, validationDiag(srcIndex, ve))
	}

	resErrs, err := resolveAll(ctx, doc, soa.ResolveAllOptions{
		OpenAPILocation:     src.Path,
		DisableExternalRefs: opts.DisableExternalRefs,
	})
	if err != nil {
		diags = append(diags, resolveDiag(srcIndex, err))
	}
	for _, re := range resErrs {
		diags = append(diags, resolveDiag(srcIndex, re))
	}

	return &Document{
		Doc: doc,
		Source: ir.SourceInfo{
			Format: "openapi@" + minor,
			Path:   src.Path,
			Hash:   sourceHash(src.Data),
		},
	}, diags, nil
}

// metaSchemaReconciledMinor is the OpenAPI minor whose schema findings are
// reconciled against the document's own meta-schema.
//
// The library defaults to the 3.1 meta-schema when no version reaches schema
// validation (jsonschema/oas3/validation.go), so a 3.1 document is checked
// correctly by accident and needs no reconciling. A 3.0 document is mis-checked
// in the other direction, and is deliberately left alone: the 3.0 meta-schema
// words the same defect differently and raises findings 3.1 does not, so
// reconciling it is a change to what a 3.0 document reports rather than the
// removal of a false positive, and belongs to its own change.
const metaSchemaReconciledMinor = "3.2"

// metaSchemaVersionArtifacts returns the validation findings the library produced
// only because it checked schema objects against the wrong meta-schema, keyed by
// findingSite.
//
// JSONSchema[T].Validate discards its options and calls Schema.Validate with none
// (jsonschema/oas3/jsonschema.go, under a "for now" comment), so the
// ParentDocumentVersion the document walk supplies never reaches schema
// validation: every schema object is checked against the 3.1 meta-schema whatever
// the document claims to be. A conformant 3.2 document writing a 3.2-only schema
// keyword — discriminator.defaultMapping is the one that surfaced — then fails
// against a meta-schema that does not declare it, and under the CLI's default
// --fail-on error a valid spec exits 1.
//
// Which findings to drop is derived, not listed. oas3.Validate does honour the
// option, so validating every schema twice — once at the document's own version,
// once as the library does — bounds the artifact exactly: a finding the second run
// raises and the first does not is the library checking against a meta-schema the
// document never claimed. Everything else is kept, including a finding on a schema
// this walk did not reach, which appears in neither run and so is never in the
// difference. Nothing names a keyword, so a 3.2 addition beyond defaultMapping is
// covered without an edit — and once the library stops dropping its options the
// two runs agree and this drops nothing.
func metaSchemaVersionArtifacts(ctx context.Context, doc *soa.OpenAPI, minor string) map[string]bool {
	if minor != metaSchemaReconciledMinor {
		return nil
	}
	version := doc.OpenAPI
	atDocumentVersion := schemaFindings(ctx, doc,
		validation.WithContextObject(&oas3.ParentDocumentVersion{OpenAPI: &version}))
	asLibraryChecks := schemaFindings(ctx, doc)
	for key := range atDocumentVersion {
		delete(asLibraryChecks, key)
	}
	return asLibraryChecks
}

// schemaFindings validates every schema object the document walk reaches under
// opts, returning each finding's site.
func schemaFindings(ctx context.Context, doc *soa.OpenAPI, opts ...validation.Option) map[string]bool {
	out := map[string]bool{}
	matchSchemas(soa.Walk(ctx, doc), func(js *oas3.JSONSchema[oas3.Referenceable]) error {
		for _, found := range oas3.Validate(ctx, js, opts...) {
			if verr, ok := asValidationError(found); ok {
				out[findingSite(verr)] = true
			}
		}
		return nil
	})
	return out
}

// matchSchemas dispatches every walk item to collect, stopping at the first match
// that reports an error.
//
// Stopping is safe rather than silent. The two runs this feeds walk the same
// document the same way, so a schema neither run reached contributes to neither
// set and can never fall into the difference between them: what is left is a
// smaller reconciliation, not a wrong one. Propagating the error instead would
// only give the caller the same choice, with a branch no input can take —
// Match returns exactly what the matcher handed it, and collect returns nil.
func matchSchemas(items iter.Seq[soa.WalkItem], collect func(*oas3.JSONSchema[oas3.Referenceable]) error) {
	for item := range items {
		if err := item.Match(soa.Matcher{Schema: collect}); err != nil {
			return
		}
	}
}

// findingSite identifies a validation finding by its rule and position, not by
// its rendered message.
//
// Two meta-schemas word the same defect differently — 3.0 omits 'null' from the
// admissible type list 3.1 prints — so comparing messages would read a rewording
// as a disappearance and drop a real finding. Rule and position are what both runs
// agree on when they are describing the same thing.
func findingSite(verr validation.Error) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d",
		verr.Rule, verr.GetDocumentLocation(), verr.GetLineNumber(), verr.GetColumnNumber())
}

// numericBoundKeywords are the schema keywords the library binds to *float64 and
// therefore mis-validates when their literal exceeds float64 range or uses a
// valid non-JSON spelling. Morphic reads these from the raw nodes instead.
var numericBoundKeywords = map[string]struct{}{
	"minimum":          {},
	"maximum":          {},
	"exclusiveMinimum": {},
	"exclusiveMaximum": {},
	"multipleOf":       {},
}

// numericLiteralArtifact reports whether a library validation finding must be
// dropped because Morphic, not the library's float64 model, is authoritative for
// numeric-bound keywords: a type-mismatch on such a keyword is always Morphic's
// to own, and an invalid-syntax finding is dropped only when caused solely by a
// recoverable non-JSON spelling (.5), never a genuinely unrepresentable literal
// (.inf).
//
// This is coupled to the speakeasy library's finding shape — TypeMismatchError's
// parent path and the offending node's YAML tag. A library upgrade that changes
// either must be revalidated against the numeric-precision conformance corpus.
func numericLiteralArtifact(verr validation.Error) bool {
	switch verr.Rule {
	case validation.RuleValidationTypeMismatch:
		return isNumericBoundKeyword(verr)
	case validation.RuleValidationInvalidSyntax:
		return invalidSyntaxOnValidNumbers(verr.GetNode())
	default:
		return false
	}
}

// isNumericBoundKeyword reports whether a type-mismatch finding concerns one of
// the numeric-bound keywords Morphic reads from the raw node, identified by the
// trailing segment of the mismatch's parent path. The library emits both a
// meta-schema and an unmarshal finding for one bad bound; both carry the keyword
// in their parent path, so both are recognized and suppressed together.
func isNumericBoundKeyword(verr validation.Error) bool {
	var mismatch *validation.TypeMismatchError
	if !errors.As(verr.UnderlyingError, &mismatch) {
		return false
	}
	name := mismatch.ParentName
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	_, ok := numericBoundKeywords[name]
	return ok
}

// invalidSyntaxOnValidNumbers reports whether an invalid-syntax finding is caused
// solely by numeric literals written in a spelling JSON rejects (.5, 5., 0644)
// that Morphic captures losslessly anyway.
//
// A literal is only a candidate cause when json.Valid rejects its source text —
// that is the grammar the library's YAML-to-JSON conversion has to satisfy, so a
// spelling JSON already accepts provoked nothing and cannot excuse the finding.
// Every rejected literal must then be one value.NumericLiteral recovers, the very
// conversion the lowerer will run; a single unrecoverable one (.inf) keeps the
// finding. Because the scan covers every numeric scalar under the node, no
// candidate cause goes unclassified.
func invalidSyntaxOnValidNumbers(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	var recovered, unrepresentable bool
	walkNumericScalars(node, 0, func(scalar *yaml.Node) {
		if json.Valid([]byte(scalar.Value)) {
			return
		}
		if _, err := value.NumericLiteral(scalar); err != nil {
			unrepresentable = true
			return
		}
		recovered = true
	})
	return recovered && !unrepresentable
}

// walkNumericScalars visits every numeric-tagged scalar reachable from node
// within maxSchemaScanDepth. Only a numeric spelling has been observed to defeat
// the library's YAML-to-JSON conversion — a custom tag or a non-string key
// converts fine — so non-numeric scalars are skipped.
func walkNumericScalars(node *yaml.Node, depth int, visit func(*yaml.Node)) {
	if node == nil || depth > maxSchemaScanDepth {
		return
	}
	if node.Kind == yaml.ScalarNode {
		if node.Tag == "!!int" || node.Tag == "!!float" {
			visit(node)
		}
		return
	}
	for _, child := range node.Content {
		walkNumericScalars(child, depth+1, visit)
	}
}

// unmarshal parses source bytes into a speakeasy document. It converts a panic
// from the third-party parser — which faults on degenerate input such as a
// whitespace-only document — into an errParse error, so the compiler upholds the
// no-panics-escape invariant instead of crashing the caller's process. The named
// returns are reset in the recover so a partially-assigned document never leaks.
func unmarshal(ctx context.Context, data []byte) (doc *soa.OpenAPI, valErrs []error, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, valErrs = nil, nil
			err = fmt.Errorf("parser panicked (%v): %w", r, errParse)
		}
	}()
	return soa.Unmarshal(ctx, bytes.NewReader(data))
}

// resolveAll resolves every reference in doc, converting a panic from the
// third-party resolver into an ordinary error — the resolve-side counterpart to
// unmarshal's barrier, needed because the resolver faults on shapes the parser
// accepts (e.g. a $ref with no value, which nil-derefs while populating the
// resolved node).
//
// The returned error joins the resolve errors, which the caller turns into
// diagnostics rather than aborting: a document that trips this is a malformed
// spec, not an I/O or programmer error. Named returns are reset in the recover
// so a partially-populated result never leaks.
func resolveAll(ctx context.Context, doc *soa.OpenAPI, opts soa.ResolveAllOptions) (resErrs []error, err error) {
	defer func() {
		if r := recover(); r != nil {
			resErrs = nil
			err = fmt.Errorf("reference resolver panicked (%v): %w", r, errParse)
		}
	}()
	return doc.ResolveAllReferences(ctx, opts)
}

// validationDiag converts one speakeasy validation error into a diagnostic. A
// structured *validation.Error yields severity, a rule-suffixed code, and
// line:col provenance; anything else degrades to an error with the bare message.
func validationDiag(srcIndex int, err error) ir.Diagnostic {
	if verr, ok := asValidationError(err); ok {
		return diag.Newf(mapSeverity(verr.Severity), diag.Validation+"/"+verr.Rule,
			validationProvenance(srcIndex, verr), "%s", verr.Error())
	}
	return diag.Newf(ir.SeverityError, diag.Validation, ir.Provenance{Source: srcIndex}, "%s", err.Error())
}

// resolveDiag converts one reference-resolution error into a diag.UnresolvedRef
// diagnostic. Resolution failures never abort lowering: the validate pass
// reports dangling references downstream.
func resolveDiag(srcIndex int, err error) ir.Diagnostic {
	prov := ir.Provenance{Source: srcIndex}
	if verr, ok := asValidationError(err); ok {
		prov = validationProvenance(srcIndex, verr)
	}
	return diag.Newf(ir.SeverityError, diag.UnresolvedRef, prov, "%s", err.Error())
}

// asValidationError extracts a structured validation error. The wrapped value
// may be stored by value or by pointer, so both forms are probed. The chain is
// walked manually with type assertions instead of errors.As; see the nolint
// comment below for why.
func asValidationError(err error) (validation.Error, bool) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		//nolint:errorlint // Deliberately hand-walked: errors.As would invoke the
		// speakeasy errors.Error type's As method, which matches any target named
		// "Error" and calls SetString, panicking on the validation.Error struct.
		switch v := e.(type) {
		case *validation.Error:
			return *v, true
		case validation.Error:
			return v, true
		}
	}
	return validation.Error{}, false
}

// validationProvenance builds line:col provenance from a validation error.
func validationProvenance(srcIndex int, e validation.Error) ir.Provenance {
	return ir.Provenance{
		Source:  srcIndex,
		Pointer: fmt.Sprintf("%d:%d", e.GetLineNumber(), e.GetColumnNumber()),
	}
}

// mapSeverity maps a speakeasy validation severity onto an ir.Severity.
func mapSeverity(s validation.Severity) ir.Severity {
	switch string(s) {
	case "warning":
		return ir.SeverityWarning
	case "hint":
		return ir.SeverityInfo
	default:
		return ir.SeverityError
	}
}

// SupportedMinor returns the normalized major.minor prefix of an OpenAPI version
// string and whether the compiler supports it (3.0, 3.1, or 3.2).
func SupportedMinor(version string) (string, bool) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return "", false
	}
	mm := parts[0] + "." + parts[1]
	switch mm {
	case "3.0", "3.1", "3.2":
		return mm, true
	default:
		return "", false
	}
}

// sourceHash returns the lowercase hex SHA-256 of the raw source bytes, used as
// the SourceInfo content hash for caching and golden-snapshot identity.
func sourceHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
