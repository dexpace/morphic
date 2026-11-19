// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package compilers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// scripted is a compiler whose compile answers what the test scripted, so each
// duty Detection.Compile checks can be broken on its own.
type scripted struct {
	stubCompiler
	doc   *ir.Document
	diags []ir.Diagnostic
	err   error
	table []ir.SourceInfo
	// got is the source Compile was handed.
	got compilers.Source
}

func (s *scripted) Compile(_ context.Context, sources []compilers.Source, _ compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	s.got = sources[0]
	return s.doc, s.diags, s.err
}

func (s *scripted) SourceTable([]compilers.Source, compilers.Options) []ir.SourceInfo { return s.table }

// spec is the source every test here compiles.
var spec = compilers.Source{Path: "spec.alpha", Data: []byte("alpha spec")}

// digest is SourceInfo.Hash for data.
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// keeping returns a compiler whose compile of spec keeps every duty: a document
// listing spec as its one source, read as alpha and hashed from its bytes.
func keeping() *scripted {
	return &scripted{
		formats: []compilers.SourceFormat{alpha},
		doc: &ir.Document{IRVersion: ir.IRVersion, Sources: []ir.SourceInfo{
			{Format: alpha.String(), Path: spec.Path, Hash: digest(spec.Data)},
		}},
		table: []ir.SourceInfo{{Path: spec.Path}},
	}
}

// detected is the detection a registry would hand back for spec claimed by
// compiler.
func detected(compiler compilers.Compiler, parsed any) compilers.Detection {
	return compilers.Detection{
		Source: spec, Compiler: compiler, Recognition: compilers.Recognition{Format: alpha, Parsed: parsed},
	}
}

func diagnosticAtSource(source int, severity ir.Severity) ir.Diagnostic {
	return ir.NewDiagnostic(severity, "alpha/finding", "a finding", ir.Provenance{Source: source})
}

func TestDetectionCompile_AKeptContractComesBackWhole(t *testing.T) {
	t.Parallel()
	scripted := keeping()
	scripted.diags = []ir.Diagnostic{diagnosticAtSource(0, ir.SeverityWarning), diagnosticAtSource(ir.NoSource, ir.SeverityInfo)}
	parse := &struct{ tree string }{"parsed"}

	out, err := detected(scripted, parse).Compile(t.Context())

	require.NoError(t, err)
	assert.Same(t, scripted.doc, out.Document)
	assert.Equal(t, scripted.diags, out.Diagnostics, "a finding at NoSource indexes no table and is kept")
	assert.Equal(t, scripted.doc.Sources, out.Sources, "a document's findings index its own table")
	assert.Same(t, parse, scripted.got.Parsed, "the compile is handed what recognizing the source parsed")
	assert.Equal(t, spec.Path, scripted.got.Path, "the compile is handed the source detection was")
	assert.Equal(t, spec.Data, scripted.got.Data)
}

// TestDetectionCompile_ADocumentMayNameWhatElseItRead pins the table's other
// end: SourceTable reads no input, so a document goes on past it to name each
// further document the compile read, and its findings may index those too.
func TestDetectionCompile_ADocumentMayNameWhatElseItRead(t *testing.T) {
	t.Parallel()
	scripted := keeping()
	read := ir.SourceInfo{Format: alpha.String(), Path: "shared.alpha", Hash: digest([]byte("shared"))}
	scripted.doc.Sources = append(scripted.doc.Sources, read)
	scripted.doc.Diagnostics = []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityWarning)}
	scripted.diags = []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityInfo)}

	out, err := detected(scripted, nil).Compile(t.Context())

	require.NoError(t, err)
	assert.Equal(t, scripted.doc.Sources, out.Sources)
	assert.Equal(t, []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityWarning), diagnosticAtSource(1, ir.SeverityInfo)}, out.Diagnostics)
}

// TestDetectionCompile_BothChannelsComeBackAsOne pins that a caller reads a
// compiler's findings from one list whichever channel it filled: the
// Document's first, then each returned one it does not hold.
func TestDetectionCompile_BothChannelsComeBackAsOne(t *testing.T) {
	t.Parallel()
	stored, both, returned := diagnosticAtSource(0, ir.SeverityError), diagnosticAtSource(0, ir.SeverityWarning), diagnosticAtSource(ir.NoSource, ir.SeverityInfo)
	scripted := keeping()
	scripted.doc.Diagnostics = []ir.Diagnostic{stored, both}
	scripted.diags = []ir.Diagnostic{both, returned}

	out, err := detected(scripted, nil).Compile(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []ir.Diagnostic{stored, both, returned}, out.Diagnostics)
}

func TestDetectionCompile_ARefusalComesBackWithItsTable(t *testing.T) {
	t.Parallel()
	scripted := keeping()
	scripted.doc = nil
	scripted.table = []ir.SourceInfo{{Path: spec.Path}, {Path: "patch.overlay"}}
	scripted.diags = []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityError)}

	out, err := detected(scripted, nil).Compile(t.Context())

	require.NoError(t, err)
	assert.Nil(t, out.Document)
	assert.Equal(t, scripted.table, out.Sources, "a refusal's findings index the table SourceTable names")
}

// TestDetectionCompile_EachBreachIsAContractError breaks one duty per case and
// requires the breach, named, as ErrContract. Every case starts from a compile
// that keeps them all, so the one edit is what fails it.
func TestDetectionCompile_EachBreachIsAContractError(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		breakIt func(*scripted)
		want    string
	}{
		"a table that does not name the source": {
			func(scriptedValue *scripted) { scriptedValue.table = nil }, "does not name the source",
		},
		"a table naming another file first": {
			func(scriptedValue *scripted) { scriptedValue.table = []ir.SourceInfo{{Path: "other"}} }, "does not name the source",
		},
		"a refusal saying nothing": {
			func(scriptedValue *scripted) { scriptedValue.doc = nil }, "no error diagnostic",
		},
		"a refusal with only a warning": {
			func(scriptedValue *scripted) {
				scriptedValue.doc, scriptedValue.diags = nil, []ir.Diagnostic{diagnosticAtSource(0, ir.SeverityWarning)}
			}, "no error diagnostic",
		},
		"a refusal finding past its table": {
			func(scriptedValue *scripted) {
				scriptedValue.doc, scriptedValue.diags = nil, []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityError)}
			}, "indexes source 1 of 1",
		},
		"a document stamped with no irVersion": {
			func(scriptedValue *scripted) { scriptedValue.doc.IRVersion = "" }, `stamped irVersion "", where this build writes "` + ir.IRVersion + `"`,
		},
		"a document stamped with another generation": {
			func(scriptedValue *scripted) { scriptedValue.doc.IRVersion = "0.0.1" }, `stamped irVersion "0.0.1"`,
		},
		"a document stamped with a newer generation": {
			func(scriptedValue *scripted) { scriptedValue.doc.IRVersion = "99.0.0" }, `stamped irVersion "99.0.0"`,
		},
		"a document stamped with a prerelease of this one": {
			func(scriptedValue *scripted) { scriptedValue.doc.IRVersion += "-rc.1" }, "-rc.1",
		},
		"a document stamped with something that is no version": {
			func(scriptedValue *scripted) { scriptedValue.doc.IRVersion = "latest" }, `stamped irVersion "latest"`,
		},
		"a document listing no source": {
			func(scriptedValue *scripted) { scriptedValue.doc.Sources = nil }, "lists 0 sources where SourceTable names 1",
		},
		"a document dropping a source the table names": {
			func(scriptedValue *scripted) {
				scriptedValue.table = append(scriptedValue.table, ir.SourceInfo{Path: "patch.overlay"})
			},
			"lists 1 sources where SourceTable names 2",
		},
		"a document naming a source the table names in another place": {
			func(scriptedValue *scripted) {
				scriptedValue.table = append(scriptedValue.table, ir.SourceInfo{Path: "patch.overlay"})
				scriptedValue.doc.Sources = append(scriptedValue.doc.Sources, ir.SourceInfo{Path: "shared.alpha"}, ir.SourceInfo{Path: "patch.overlay"})
			},
			`source 1 is "shared.alpha" where SourceTable names "patch.overlay"`,
		},
		"a document naming the source by another path": {
			func(scriptedValue *scripted) { scriptedValue.doc.Sources[0].Path = "elsewhere" }, `source 0 is "elsewhere"`,
		},
		"a document reading the source as no format": {
			func(scriptedValue *scripted) { scriptedValue.doc.Sources[0].Format = "" }, `reads source 0 as ""`,
		},
		"a document reading the source as another format": {
			func(scriptedValue *scripted) { scriptedValue.doc.Sources[0].Format = "alpha@2" }, `reads source 0 as "alpha@2"`,
		},
		"a document hashing nothing": {
			func(scriptedValue *scripted) { scriptedValue.doc.Sources[0].Hash = "" }, `hashes source 0 as ""`,
		},
		"a document hashing other bytes": {
			func(scriptedValue *scripted) { scriptedValue.doc.Sources[0].Hash = digest([]byte("other")) }, "hashes source 0",
		},
		"a finding on the document past its table": {
			func(scriptedValue *scripted) {
				scriptedValue.doc.Diagnostics = []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityWarning)}
			}, "on the document",
		},
		"a returned finding past the table": {
			func(scriptedValue *scripted) {
				scriptedValue.diags = []ir.Diagnostic{diagnosticAtSource(1, ir.SeverityWarning)}
			}, "indexes source 1 of 1",
		},
		"a returned finding at a sentinel the IR does not declare": {
			func(scriptedValue *scripted) {
				scriptedValue.diags = []ir.Diagnostic{diagnosticAtSource(-2, ir.SeverityWarning)}
			}, "indexes source -2",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			scripted := keeping()
			tc.breakIt(scripted)

			out, err := detected(scripted, nil).Compile(t.Context())

			require.ErrorIs(t, err, compilers.ErrContract)
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "compile as alpha@1", "the error names the format compiled")
			assert.Equal(t, compilers.Compiled{}, out, "nothing of a broken compile comes back")
		})
	}
}

// TestDetectionCompile_HandsBackCopiesOfWhatTheCompilerAnsweredWith pins that
// the slices a compiler answered with are the compiler's to keep: changing
// them after the compile changes nothing the caller was handed. The Document is
// the one thing a compile gives up.
func TestDetectionCompile_HandsBackCopiesOfWhatTheCompilerAnsweredWith(t *testing.T) {
	t.Parallel()
	scripted := keeping()
	scripted.doc = nil
	scripted.table = []ir.SourceInfo{{Path: spec.Path}}
	scripted.diags = []ir.Diagnostic{diagnosticAtSource(0, ir.SeverityError)}

	out, err := detected(scripted, nil).Compile(t.Context())
	require.NoError(t, err)
	scripted.table[0].Path = "tampered"
	scripted.diags[0].Code = "tampered"

	assert.Equal(t, spec.Path, out.Sources[0].Path)
	assert.Equal(t, "alpha/finding", out.Diagnostics[0].Code)
}

func TestDetectionCompile_ACompilerErrorPassesThroughUnwrapped(t *testing.T) {
	t.Parallel()
	scripted := keeping()
	scripted.err = context.Canceled

	_, err := detected(scripted, nil).Compile(t.Context())

	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, compilers.ErrContract, "a compiler that stops when asked keeps its contract")
}

func TestDetectionCompile_ADetectionNamingNoCompilerIsAnError(t *testing.T) {
	t.Parallel()
	for name, det := range map[string]compilers.Detection{
		"the zero detection": {},
		"a typed nil":        {Compiler: (*scripted)(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := det.Compile(t.Context())
			require.Error(t, err)
			assert.NotErrorIs(t, err, compilers.ErrContract, "no compiler is there to have broken one")
		})
	}
}

func TestRegistry_RegisterRefusesAFormatNamingNoDialect(t *testing.T) {
	t.Parallel()
	for name, format := range map[string]compilers.SourceFormat{
		"no name":         {Version: "1"},
		"no version":      {Name: "alpha"},
		"the zero format": {},
		"an @ in a name":  {Name: "al@pha", Version: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			err := reg.Register(&stubCompiler{formats: []compilers.SourceFormat{alpha, format}})
			require.ErrorIs(t, err, compilers.ErrContract)
			assert.Contains(t, err.Error(), "names no dialect")
			assert.Empty(t, reg.Formats(), "on failure nothing is registered")
		})
	}
}

// TestRegistry_DetectRefusesAClaimNamingAPartFormat holds a claim to the
// predicate Register holds a format to, not only to being non-zero: a claim
// naming half a dialect would reach the user as a format no compiler serves.
func TestRegistry_DetectRefusesAClaimNamingAPartFormat(t *testing.T) {
	t.Parallel()
	for name, format := range map[string]compilers.SourceFormat{
		"no version":     {Name: "alpha"},
		"no name":        {Version: "1"},
		"an @ in a name": {Name: "al@pha", Version: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			require.NoError(t, reg.Register(&stubCompiler{
				formats: []compilers.SourceFormat{alpha}, marker: "alpha", detects: format,
			}))

			_, ok, err := reg.Detect(t.Context(), spec, compilers.Configuration{})

			require.ErrorIs(t, err, compilers.ErrContract)
			assert.False(t, ok)
		})
	}
}

func TestRegistry_RegisterRefusesNoFormatsAsAContractError(t *testing.T) {
	t.Parallel()
	err := compilers.NewRegistry().Register(&stubCompiler{})
	require.ErrorIs(t, err, compilers.ErrContract)
}

// TestRegistry_DetectRefusesADeclineIndexingAnotherSource pins the table a
// decline may index: the one source detection is handed, or no source at all.
func TestRegistry_DetectRefusesADeclineIndexingAnotherSource(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		source int
		broken bool
	}{
		"the source":       {source: 0},
		"no source":        {source: ir.NoSource},
		"a second source":  {source: 1, broken: true},
		"a negative index": {source: -3, broken: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			require.NoError(t, reg.Register(&stubCompiler{
				formats:  []compilers.SourceFormat{alpha},
				declines: []ir.Diagnostic{diagnosticAtSource(tc.source, ir.SeverityError)},
			}))

			det, _, err := reg.Detect(t.Context(), spec, compilers.Configuration{})

			if !tc.broken {
				require.NoError(t, err)
				assert.Len(t, det.Declined, 1, "the decline is carried out for the caller")
				return
			}
			require.ErrorIs(t, err, compilers.ErrContract)
			assert.Contains(t, err.Error(), "alpha@1", "the error names the compiler")
		})
	}
}

// TestRegistry_DetectRecordsTheSourceItWasHanded pins the source a Detection
// compiles to the one it was detected from, on every answer Detect gives, so no
// caller hands the compile a second input.
func TestRegistry_DetectRecordsTheSourceItWasHanded(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"a source a compiler takes":              "alpha source",
		"a source every compiler declines":       "gamma source",
		"a source recognized and served by none": "beta source",
		"a source with no bytes":                 "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			require.NoError(t, reg.Register(&stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}))
			require.NoError(t, reg.Register(&stubCompiler{
				formats: []compilers.SourceFormat{{Name: "gamma", Version: "1"}}, marker: "beta",
				detects: compilers.SourceFormat{Name: "beta", Version: "1"},
			}))
			src := compilers.Source{Path: "in." + name, Data: []byte(data)}

			det, _, err := reg.Detect(t.Context(), src, compilers.Configuration{})

			require.NoError(t, err)
			assert.Equal(t, src, det.Source)
		})
	}
}

// TestRegistry_DetectRefusesADeclineSpeakingWithNoError holds a decline to the
// duty a refusal has: a compiler that speaks while declining its own source
// says why with an error, or the caller is left no document and no reason.
// Saying nothing is how it declines bytes that are not its own.
func TestRegistry_DetectRefusesADeclineSpeakingWithNoError(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		declines []ir.Diagnostic
		broken   bool
	}{
		"silence":                   {},
		"an error":                  {declines: []ir.Diagnostic{diagnosticAtSource(0, ir.SeverityError)}},
		"a warning beside an error": {declines: []ir.Diagnostic{diagnosticAtSource(0, ir.SeverityWarning), diagnosticAtSource(0, ir.SeverityError)}},
		"a warning alone":           {declines: []ir.Diagnostic{diagnosticAtSource(0, ir.SeverityWarning)}, broken: true},
		"an info alone":             {declines: []ir.Diagnostic{diagnosticAtSource(ir.NoSource, ir.SeverityInfo)}, broken: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			require.NoError(t, reg.Register(&stubCompiler{formats: []compilers.SourceFormat{alpha}, declines: tc.declines}))

			det, ok, err := reg.Detect(t.Context(), spec, compilers.Configuration{})

			assert.False(t, ok)
			if !tc.broken {
				require.NoError(t, err)
				assert.Equal(t, tc.declines, det.Declined, "the decline is carried out for the caller")
				return
			}
			require.ErrorIs(t, err, compilers.ErrContract)
			assert.Contains(t, err.Error(), "no error diagnostic saying why")
			assert.Contains(t, err.Error(), "alpha@1", "the error names the compiler")
		})
	}
}
