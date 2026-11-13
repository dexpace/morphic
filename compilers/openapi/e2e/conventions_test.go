// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package e2e_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/compilers/openapi"
	"github.com/dexpace/morphic/compilers/openapi/internal/openapitest"
	"github.com/dexpace/morphic/ir"
)

// conventionCases are the corpus specs of the policies that read what a
// document's names imply, each of which fills a fact on an operation, a
// parameter or a header as well as on a member.
var conventionCases = []string{
	"idempotency-inference", "secret-inference", "replacement-inference", "http-date-inference",
}

// conventionMarks are the markers those policies leave on what they fill.
var conventionMarks = []ir.Inferred{openapi.IdempotencyInferenceHeuristic, openapi.SecretInferenceHeuristic,
	openapi.HTTPDateInferenceHeuristic, openapi.ReplacementInferenceHeuristic}

// conventionCodes are what those policies report.
var conventionCodes = []string{"openapi/undecided-idempotency", "openapi/undecided-replacement",
	"openapi/inferred-http-date"}

// useFacts is what the conventions fill on one operation, keyed by the ID of
// the operation, parameter or header holding each: the order oracle compares
// the type registry alone, which holds none of these.
type useFacts struct {
	Idempotency ir.Idempotency
	Secret      bool
	Encoding    *ir.Encoding
	ReplacedBy  []ir.EntityRef
	Inferred    ir.Inferred
}

// conventionFacts gathers useFacts for every operation of doc, its
// parameters and its responses' headers.
func conventionFacts(doc *ir.Document) map[string]useFacts {
	replaced := func(d *ir.Deprecation) []ir.EntityRef {
		if d == nil {
			return nil
		}
		return d.ReplacedBy
	}
	facts := map[string]useFacts{}
	for _, op := range allOperations(doc) {
		facts[string(op.ID)] = useFacts{Idempotency: op.Idempotency, ReplacedBy: replaced(op.Deprecation),
			Inferred: op.Provenance.Inferred}
		for _, p := range op.Params {
			facts[string(p.ID)] = useFacts{Secret: p.Secret, ReplacedBy: replaced(p.Deprecation),
				Inferred: p.Provenance.Inferred}
		}
		for _, r := range op.Responses {
			for _, h := range r.Headers {
				facts[string(h.ID)] = useFacts{Secret: h.Secret, Encoding: h.Encoding, Inferred: h.Provenance.Inferred}
			}
		}
	}
	return facts
}

// TestConventions_DeclarationOrderChangesNothing compiles each convention's
// case as written and with every mapping reversed, its paths, methods,
// headers and members among them, and requires the same facts on each
// operation, parameter and header, and the same reports, so no reading turns
// on which of two declarations lowered first: a replacement declared after
// what it replaces is named as one declared before it is.
func TestConventions_DeclarationOrderChangesNothing(t *testing.T) {
	t.Parallel()
	for _, name := range conventionCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
			require.NoError(t, err)
			written, writtenDiags := parseFull(t, string(src))
			backward, backwardDiags := parseFull(t, string(reversed(t, string(src))))
			facts := conventionFacts(written)
			assert.Empty(t, cmp.Diff(facts, conventionFacts(backward)))
			assert.Empty(t, cmp.Diff(conventionReports(writtenDiags), conventionReports(backwardDiags)))
			filled := 0
			for _, f := range facts {
				for _, mark := range conventionMarks {
					if strings.Contains(string(f.Inferred), string(mark)) {
						filled++
					}
				}
			}
			assert.Positive(t, filled, "the case fills a fact for the comparison to read")
		})
	}
}

// conventionReports is each of diags the conventions report, by its code,
// pointer and message, sorted: a reversed source moves every line.
func conventionReports(diags []ir.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if slices.Contains(conventionCodes, d.Code) {
			out = append(out, d.Code+" "+string(d.Provenance.Pointer)+" "+d.Message)
		}
	}
	slices.Sort(out)
	return out
}

// TestConventions_DisabledFillsNothing compiles each convention's case with
// every one of the four policies off, and requires none of their marks and
// none of their reports, so each switch reaches the compile through the
// public Options.
func TestConventions_DisabledFillsNothing(t *testing.T) {
	t.Parallel()
	opts := openapi.Options{
		IdempotencyInference: openapi.IdempotencyInference{Disabled: true},
		SecretInference:      openapi.SecretInference{Disabled: true},
		HTTPDateInference:    openapi.HTTPDateInference{Disabled: true},
		ReplacementInference: openapi.ReplacementInference{Disabled: true},
	}
	for _, name := range conventionCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(filepath.Join(conformanceDir, name+".yaml"))
			require.NoError(t, err)
			doc, diags, err := openapi.New().Compile(t.Context(), []compilers.Source{openapitest.SourceOf(string(src))},
				compilers.Options{FormatOptions: opts})
			require.NoError(t, err)
			require.NotNil(t, doc)
			for _, code := range conventionCodes {
				assert.False(t, openapitest.HasDiag(diags, code), code)
			}
			for id, f := range conventionFacts(doc) {
				for _, mark := range conventionMarks {
					assert.NotContains(t, string(f.Inferred), string(mark), id)
				}
			}
			for id, td := range doc.Types {
				m, ok := td.(*ir.Model)
				if !ok {
					continue
				}
				for _, p := range m.Properties {
					for _, mark := range conventionMarks {
						assert.NotContains(t, string(p.Provenance.Inferred), string(mark), "%s %s", id, p.ID)
					}
				}
			}
		})
	}
}
