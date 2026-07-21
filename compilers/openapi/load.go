package openapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	soa "github.com/speakeasy-api/openapi/openapi"
	"github.com/speakeasy-api/openapi/validation"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// errParse marks a hard failure to parse a source document — an I/O- or
// programmer-level error, distinct from a spec problem reported as a diagnostic.
var errParse = errors.New("parse source")

// loaded is the successful output of the load phase: a parsed, resolved
// speakeasy document plus the identity metadata the rest of the compiler needs.
// A nil *loaded with error-severity diagnostics means the document is a spec
// problem the compiler refuses to lower (e.g. an unsupported version).
type loaded struct {
	Doc    *soa.OpenAPI           // parsed, reference-resolved document
	Format compilers.SourceFormat // "openapi" + normalized major.minor
	Source ir.SourceInfo          // format tag, path, content hash
}

// load parses, validates, and resolves one source document. Spec problems
// become ir.Diagnostic values; the Go error return is reserved for I/O and
// programmer errors (a hard unmarshal failure). A nil document with diagnostics
// signals a refusal to lower (unsupported version) without aborting the batch.
//
//nolint:unparam // srcIndex varies once Compile drives the multi-source loop
func load(ctx context.Context, srcIndex int, src compilers.Source, opts Options) (*loaded, []ir.Diagnostic, error) {
	doc, valErrs, err := unmarshal(ctx, src.Data)
	if err != nil {
		return nil, nil, fmt.Errorf("openapi: unmarshal source %d: %w", srcIndex, err)
	}

	minor, ok := supportedMinor(doc.OpenAPI)
	if !ok {
		return nil, []ir.Diagnostic{diagf(ir.SeverityError, codeUnsupportedVersion,
			ir.Provenance{Source: srcIndex},
			"unsupported OpenAPI version %q; want 3.0, 3.1, or 3.2", doc.OpenAPI)}, nil
	}

	var diags []ir.Diagnostic
	for _, ve := range valErrs {
		diags = append(diags, validationDiag(srcIndex, ve))
	}

	resErrs, err := doc.ResolveAllReferences(ctx, soa.ResolveAllOptions{
		OpenAPILocation:     src.Path,
		DisableExternalRefs: opts.DisableExternalRefs,
	})
	if err != nil {
		diags = append(diags, resolveDiag(srcIndex, err))
	}
	for _, re := range resErrs {
		diags = append(diags, resolveDiag(srcIndex, re))
	}

	return &loaded{
		Doc:    doc,
		Format: compilers.SourceFormat{Name: "openapi", Version: minor},
		Source: ir.SourceInfo{
			Format: "openapi@" + minor,
			Path:   src.Path,
			Hash:   sourceHash(src.Data),
		},
	}, diags, nil
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

// validationDiag converts one speakeasy validation error into a diagnostic. A
// structured *validation.Error yields severity, a rule-suffixed code, and
// line:col provenance; anything else degrades to an error with the bare message.
func validationDiag(srcIndex int, err error) ir.Diagnostic {
	if verr, ok := asValidationError(err); ok {
		return diagf(mapSeverity(verr.Severity), codeValidation+"/"+verr.Rule,
			validationProvenance(srcIndex, verr), "%s", verr.Error())
	}
	return diagf(ir.SeverityError, codeValidation, ir.Provenance{Source: srcIndex}, "%s", err.Error())
}

// resolveDiag converts one reference-resolution error into a codeUnresolvedRef
// diagnostic. Resolution failures never abort lowering: the validate pass
// reports dangling references downstream.
func resolveDiag(srcIndex int, err error) ir.Diagnostic {
	prov := ir.Provenance{Source: srcIndex}
	if verr, ok := asValidationError(err); ok {
		prov = validationProvenance(srcIndex, verr)
	}
	return diagf(ir.SeverityError, codeUnresolvedRef, prov, "%s", err.Error())
}

// asValidationError extracts a structured validation error. The wrapped value
// may be stored by value or by pointer, so both forms are probed. The error
// chain is walked manually rather than via errors.As: the speakeasy errors.Error
// string type has an As method that matches any target type named "Error" and
// calls SetString on it, which panics on the validation.Error struct (also named
// "Error"). Manual unwrapping with type assertions never invokes that As method.
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

// supportedMinor returns the normalized major.minor prefix of an OpenAPI version
// string and whether the compiler supports it (3.0, 3.1, or 3.2).
func supportedMinor(version string) (string, bool) {
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
