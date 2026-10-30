// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package irverify_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dexpace/morphic/ir"
	"github.com/dexpace/morphic/ir/irverify"
)

// tabledDoc returns a one-type document that declares everything a compiled
// document carries: its namespaces, one service and one source. Each test below
// breaks exactly one of those and holds the rest to staying quiet.
func tabledDoc() *ir.Document {
	doc := validDoc()
	doc.Services = []ir.Service{fixtureService(fixtureSpace)}
	doc.Sources = oneSource()
	doc.IDSpaces = map[string][]string{ir.IDKindType: {fixtureSpace}, ir.IDKindService: {fixtureSpace}}
	return doc
}

func TestVerify_DocumentWithItsTablesIsClean(t *testing.T) {
	t.Parallel()
	assert.Empty(t, irverify.Verify(tabledDoc()))
}

func TestVerify_DocumentWithoutServicesIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		services []ir.Service
	}{
		{"nil", nil},
		{"empty", []ir.Service{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := tabledDoc()
			doc.Services = tc.services

			got := irverify.Verify(doc)
			assert.Equal(t, []string{"ir/services-absent"}, codesOf(got))
			assert.Equal(t, []string{"doc.Services"}, violationPaths(got, "ir/services-absent"))
		})
	}
}

func TestVerify_DocumentWithoutSourcesIsAViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sources []ir.SourceInfo
	}{
		{"nil", nil},
		{"empty", []ir.SourceInfo{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := tabledDoc()
			doc.Sources = tc.sources

			got := irverify.Verify(doc)
			assert.Equal(t, []string{"ir/sources-absent"}, codesOf(got))
			assert.Equal(t, []string{"doc.Sources"}, violationPaths(got, "ir/sources-absent"))
		})
	}
}

// TestVerify_SourceFormatMustBeANameAndAVersion is the table for the format
// grammar: each part present on either side of the '@', with the spellings the
// compilers write beside the ones that miss a part.
func TestVerify_SourceFormatMustBeANameAndAVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format string
		clean  bool
	}{
		{"empty", "", false},
		{"no separator", "openapi", false},
		{"no name", "@3.1", false},
		{"no version", "openapi@", false},
		{"separator alone", "@", false},
		{"a document format", "openapi@3.1", true},
		{"an overlay format", "overlay@1.0.0", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := tabledDoc()
			doc.Sources[0].Format = tc.format

			got := irverify.Verify(doc)
			if tc.clean {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{"ir/source-format-malformed"}, codesOf(got))
			assert.Equal(t, []string{"doc.Sources[0].Format"}, violationPaths(got, "ir/source-format-malformed"))
		})
	}
}

// TestVerify_SourceHashMustBeLowercaseSHA256Hex is the table for the digest
// grammar: the length on both sides of 64, an uppercase digit, and the
// characters either side of each accepted range, with a real digest as the
// clean row. The digest holds a 0, a 9, an a and an f, so a range check
// tightened at either end reddens the clean row.
func TestVerify_SourceHashMustBeLowercaseSHA256Hex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		hash  string
		clean bool
	}{
		{"empty", "", false},
		{"a placeholder", "h", false},
		{"one short", emptySHA256[1:], false},
		{"one long", emptySHA256 + "0", false},
		{"uppercase", strings.ToUpper(emptySHA256), false},
		{"a non-hex letter", "g" + emptySHA256[1:], false},
		{"below the digits", "/" + emptySHA256[1:], false},
		{"above the digits", ":" + emptySHA256[1:], false},
		{"below the letters", "`" + emptySHA256[1:], false},
		{"a non-ASCII rune", "é" + emptySHA256[2:], false},
		{"a real digest", emptySHA256, true},
		{"all zeros", strings.Repeat("0", 64), true},
		{"all f", strings.Repeat("f", 64), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := tabledDoc()
			doc.Sources[0].Hash = tc.hash

			got := irverify.Verify(doc)
			if tc.clean {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, []string{"ir/source-hash-malformed"}, codesOf(got))
			assert.Equal(t, []string{"doc.Sources[0].Hash"}, violationPaths(got, "ir/source-hash-malformed"))
		})
	}
}

// TestVerify_SourceFindingsNameTheEntryAtFault pins the index in the path: a
// breach is reported at the source that has it, whichever place that is in the
// table, and the clean entries beside it draw nothing.
func TestVerify_SourceFindingsNameTheEntryAtFault(t *testing.T) {
	t.Parallel()
	good := ir.SourceInfo{Format: "openapi@3.1", Path: "api.yaml", Hash: emptySHA256}
	overlay := ir.SourceInfo{Format: "overlay@1.0.0", Path: "patch.yaml", Hash: emptySHA256}
	tests := []struct {
		name    string
		sources []ir.SourceInfo
		want    []string
	}{
		{
			name:    "the second breaks both rules",
			sources: []ir.SourceInfo{good, {Format: "overlay", Path: "patch.yaml", Hash: "h"}},
			want:    []string{"doc.Sources[1].Format", "doc.Sources[1].Hash"},
		},
		{
			name:    "the first breaks the format",
			sources: []ir.SourceInfo{{Format: "openapi", Path: "api.yaml", Hash: emptySHA256}, overlay},
			want:    []string{"doc.Sources[0].Format"},
		},
		{
			name: "each breaks the hash",
			sources: []ir.SourceInfo{
				{Format: "openapi@3.1", Path: "api.yaml", Hash: "h"},
				{Format: "overlay@1.0.0", Path: "patch.yaml", Hash: "h"},
			},
			want: []string{"doc.Sources[0].Hash", "doc.Sources[1].Hash"},
		},
		{name: "neither breaks one", sources: []ir.SourceInfo{good, overlay}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := tabledDoc()
			doc.Sources = tc.sources

			found := irverify.Verify(doc)
			got := make([]string, 0, len(found))
			for _, v := range found {
				got = append(got, v.Path)
			}
			if len(tc.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
