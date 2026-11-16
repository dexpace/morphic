// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/pass"
)

// The cases of a three-case product: nothing written, a keyword, and its other
// value. A case's source holds its name, which stands in below for what the
// case compiles to.
const (
	caseNone  = 0
	caseWrite = 1
	caseOther = 2
)

// presenceProduct is a product of the three cases, each writing the document
// write gives it, with its source holding its name.
func presenceProduct(write func(i int) Document) fake {
	axes := []Axis{coreAxis("c", "none", "kw", "kw~")}
	return fake{axes: axes, write: func(at []int) (Document, bool) {
		doc := write(at[0])
		doc.Sources = []compilers.Source{{Path: "spec.json", Data: []byte(axes[0].Values[at[0]].Name)}}
		return doc, true
	}}
}

// carriedDocs writes the keyword as a carried one: its case names the absent
// and the other case, as the cases of a format do.
func carriedDocs(i int) Document {
	if i != caseWrite {
		return Document{}
	}
	return Document{Absent: []int{caseNone}, Other: []int{caseOther}, Carried: true}
}

// swapDocuments makes each case compile to the document of the source's name
// in docs, drawing the diagnostics diags names, and a compile error where fail
// names the source.
func swapDocuments(t *testing.T, docs map[string]*ir.Document, diags map[string][]ir.Diagnostic, fail string) {
	t.Helper()
	swapCompile(t, func(_ context.Context, _ string, data []byte, _ []pass.Pass) (*ir.Document, []ir.Diagnostic, error) {
		if string(data) == fail {
			return nil, nil, assert.AnError
		}
		return docs[string(data)], diags[string(data)], nil
	})
}

// named returns the sound document titled name, as the case of that name compiles to.
func named(name string) *ir.Document {
	doc := soundDoc()
	doc.Name = name
	return doc
}

// presenceOf returns the presence verdict of the keyword's case of product p.
func presenceOf(t *testing.T, p fake) Verdict {
	t.Helper()
	res, ok := CheckCase(t.Context(), p, []int{caseWrite})
	require.True(t, ok)
	require.Equal(t, "kw", res.Spec)
	for _, v := range res.Verdicts {
		if v.Oracle == OraclePresence {
			return v
		}
	}
	t.Fatal("no presence verdict")
	return Verdict{}
}

// TestPresent_JudgesAKeywordAgainstItsNeighbours holds each outcome of the
// oracle to the three documents a case compiles to, as fake compiles stand
// for them: a name written is a document of that name.
func TestPresent_JudgesAKeywordAgainstItsNeighbours(t *testing.T) {
	for _, tc := range []struct {
		name    string
		write   func(i int) Document
		docs    map[string]string // what each case compiles to
		outcome Outcome
		detail  string
	}{
		{"carried and differing from both", carriedDocs,
			map[string]string{"none": "a", "kw": "b", "kw~": "c"}, OutcomeOK, ""},
		{"carried and equal to absent", carriedDocs,
			map[string]string{"none": "a", "kw": "a", "kw~": "c"}, OutcomeNotCarried, "changes nothing\nequal to none"},
		{"carried and equal to its other value", carriedDocs,
			map[string]string{"none": "a", "kw": "b", "kw~": "b"}, OutcomeNotCarried, "value discarded\nequal to kw~"},
		{"carried and equal to everything", carriedDocs,
			map[string]string{"none": "a", "kw": "a", "kw~": "a"}, OutcomeNotCarried, "changes nothing\nequal to none"},
		{"carried with no other value", func(i int) Document {
			doc := carriedDocs(i)
			doc.Other = nil
			return doc
		}, map[string]string{"none": "a", "kw": "b", "kw~": "b"}, OutcomeOK, ""},
		{"replacing and differing from its other value", replacingDocs,
			map[string]string{"none": "b", "kw": "b", "kw~": "c"}, OutcomeOK, ""},
		{"replacing and equal to its other value", replacingDocs,
			map[string]string{"none": "a", "kw": "b", "kw~": "b"}, OutcomeNotCarried, "value discarded\nequal to kw~"},
		{"not carried and equal to absent", notCarriedDocs,
			map[string]string{"none": "a", "kw": "a", "kw~": "c"}, OutcomeOK, ""},
		{"not carried and differing from absent", notCarriedDocs,
			map[string]string{"none": "a", "kw": "b", "kw~": "b"}, OutcomeNotCarried,
			"carried\nwhy not\ndiffers from none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docs := map[string]*ir.Document{}
			for n, title := range tc.docs {
				docs[n] = named(title)
			}
			swapDocuments(t, docs, nil, "")

			v := presenceOf(t, presenceProduct(tc.write))

			assert.True(t, v.Asked())
			assert.Equal(t, tc.outcome, v.Outcome)
			assert.Equal(t, tc.detail, v.Detail)
		})
	}
}

// replacingDocs writes the keyword as one replacing a member its target
// writes: the none case writes the keyword too, so it is no neighbour to compare.
func replacingDocs(i int) Document {
	doc := carriedDocs(i)
	doc.Replaces = true
	return doc
}

// notCarriedDocs writes the keyword as one nothing carries, with a reason.
func notCarriedDocs(i int) Document {
	if i != caseWrite {
		return Document{}
	}
	return Document{Absent: []int{caseNone}, Reason: "why not"}
}

// TestPresent_ComparesTheDocumentAlone holds the comparison to what the case
// says: a difference in the diagnostics or in the source table is none, so a
// keyword announced and dropped is found, and a case whose source text differs
// from its neighbour's does not read as different.
func TestPresent_ComparesTheDocumentAlone(t *testing.T) {
	announced := []ir.Diagnostic{{Severity: ir.SeverityWarning, Code: "openapi/validation-no-effect", Message: "noticed"}}
	docs := map[string]*ir.Document{"none": named("a"), "kw": named("a"), "kw~": named("c")}
	docs["kw"].Sources = []ir.SourceInfo{{Format: "openapi@3.1", Path: "api.yaml", Hash: "deadbeef"}}
	docs["kw"].Diagnostics = announced
	swapDocuments(t, docs, map[string][]ir.Diagnostic{"kw": announced}, "")

	v := presenceOf(t, presenceProduct(carriedDocs))

	assert.Equal(t, OutcomeNotCarried, v.Outcome, "a diagnostic and a source hash are no difference")
	assert.Equal(t, "changes nothing\nequal to none", v.Detail)
}

// TestPresent_LeavesTheKeptDocumentAsItWas holds the comparison to a copy: a
// shard keeps each document for the oracles after this one.
func TestPresent_LeavesTheKeptDocumentAsItWas(t *testing.T) {
	docs := map[string]*ir.Document{"none": named("a"), "kw": named("b"), "kw~": named("c")}
	diags := map[string][]ir.Diagnostic{"kw": {{Severity: ir.SeverityInfo, Code: "x/y", Message: "m"}}}
	docs["kw"].Diagnostics = diags["kw"]
	swapDocuments(t, docs, diags, "")
	p := presenceProduct(carriedDocs)
	s := newShard(p, p.axes, []int{caseWrite})

	_, ok := s.check(t.Context(), []int{caseWrite})
	require.True(t, ok)

	kept, ok := s.compile(t.Context(), []int{caseWrite})
	require.True(t, ok)
	assert.NotEmpty(t, kept.doc.Sources)
	assert.Equal(t, diags["kw"], kept.diags)
	assert.Equal(t, diags["kw"], kept.doc.Diagnostics)
}

func TestPresent_DeclinesWhatItCannotCompare(t *testing.T) {
	boom := []ir.Diagnostic{{Severity: ir.SeverityError, Code: "x/refused", Message: "refused"}}
	same := map[string]*ir.Document{"none": named("a"), "kw": named("b"), "kw~": named("c")}
	for _, tc := range []struct {
		name  string
		write func(i int) Document
		diags map[string][]ir.Diagnostic
		fail  string
		want  Decline
	}{
		{"an error diagnostic on the keyword's case", carriedDocs, map[string][]ir.Diagnostic{"kw": boom}, "", DeclineErrorDiagnostic},
		{"an error diagnostic on the absent case", carriedDocs, map[string][]ir.Diagnostic{"none": boom}, "", DeclineErrorDiagnostic},
		{"an error diagnostic on the other case", carriedDocs, map[string][]ir.Diagnostic{"kw~": boom}, "", DeclineErrorDiagnostic},
		{"an error diagnostic beside a keyword nothing carries", notCarriedDocs, map[string][]ir.Diagnostic{"none": boom}, "", DeclineErrorDiagnostic},
		{"a compile that fails", carriedDocs, nil, "none", DeclineNoDocument},
		{"a neighbour outside the product", func(i int) Document {
			doc := carriedDocs(i)
			if i == caseWrite {
				doc.Other = []int{caseOther + 1}
			}
			return doc
		}, nil, "", DeclineNoDocument},
		{"a case that names no neighbour", func(int) Document { return Document{} }, nil, "", DeclineNoConstruct},
	} {
		t.Run(tc.name, func(t *testing.T) {
			swapDocuments(t, same, tc.diags, tc.fail)

			v := presenceOf(t, presenceProduct(tc.write))

			assert.Equal(t, declined(OraclePresence, tc.want), v)
		})
	}
}

// TestPresent_AnErrorDiagnosticBesideAnUnreadNeighbourIsNotAsked holds the
// decline to the documents the case compares: a control's other case, which
// no verdict reads, draws its error unread.
func TestPresent_AnErrorDiagnosticBesideAnUnreadNeighbourIsNotAsked(t *testing.T) {
	boom := []ir.Diagnostic{{Severity: ir.SeverityError, Code: "x/refused", Message: "refused"}}
	docs := map[string]*ir.Document{"none": named("a"), "kw": named("a"), "kw~": named("c")}
	swapDocuments(t, docs, map[string][]ir.Diagnostic{"kw~": boom}, "")

	v := presenceOf(t, presenceProduct(notCarriedDocs))

	assert.Equal(t, passed(OraclePresence), v)
}

// TestPresent_AssertsWhatACaseMustSay holds the oracle to what a case's
// document promises: a keyword nothing carries gives a reason, and each
// document it compares encodes.
func TestPresent_AssertsWhatACaseMustSay(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(i int) Document
		docs  map[string]*ir.Document
		want  string
	}{
		{"nothing carries it and says not why", func(i int) Document {
			doc := notCarriedDocs(i)
			doc.Reason = ""
			return doc
		}, map[string]*ir.Document{"none": named("a"), "kw": named("a")}, "says not why"},
		{"a replacement naming no other value", func(i int) Document {
			doc := replacingDocs(i)
			doc.Other = nil
			return doc
		}, map[string]*ir.Document{"none": named("a"), "kw": named("b"), "kw~": named("c")}, "names no other value"},
		{"a replacement nothing carries", func(i int) Document {
			doc := notCarriedDocs(i)
			doc.Replaces = true
			return doc
		}, map[string]*ir.Document{"none": named("a"), "kw": named("a")}, "names no other value"},
		{"a document that does not encode", carriedDocs,
			map[string]*ir.Document{"none": named("a"), "kw": badExtDoc(), "kw~": named("c")}, "does not encode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			swapDocuments(t, tc.docs, nil, "")
			res, ok := CheckCase(t.Context(), presenceProduct(tc.write), []int{caseWrite})
			require.True(t, ok)
			assert.Equal(t, OutcomePanic, res.Outcome)
			assert.Contains(t, res.Detail, tc.want)
		})
	}
}

// TestPresent_ANeighbourTheCasesWriteNothingForIsAPanic holds a neighbour a
// document names to a case the product writes.
func TestPresent_ANeighbourTheCasesWriteNothingForIsAPanic(t *testing.T) {
	docs := map[string]*ir.Document{"none": named("a"), "kw": named("b"), "kw~": named("c")}
	swapDocuments(t, docs, nil, "")
	written := presenceProduct(carriedDocs)
	p := fake{axes: written.axes, write: func(at []int) (Document, bool) {
		if at[0] == caseNone {
			return Document{}, false
		}
		return written.write(at)
	}}

	res, ok := CheckCase(t.Context(), p, []int{caseWrite})

	require.True(t, ok)
	assert.Equal(t, OutcomePanic, res.Outcome)
	assert.Contains(t, res.Detail, "writes nothing")
}
