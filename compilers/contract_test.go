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

// detected is the detection a registry would hand back for spec claimed by c.
func detected(c compilers.Compiler, parsed any) compilers.Detection {
	return compilers.Detection{
		Source: spec, Compiler: c, Recognition: compilers.Recognition{Format: alpha, Parsed: parsed},
	}
}

func at(source int, severity ir.Severity) ir.Diagnostic {
	return ir.NewDiagnostic(severity, "alpha/finding", "a finding", ir.Provenance{Source: source})
}

func TestDetectionCompile_AKeptContractComesBackWhole(t *testing.T) {
	t.Parallel()
	c := keeping()
	c.diags = []ir.Diagnostic{at(0, ir.SeverityWarning), at(ir.NoSource, ir.SeverityInfo)}
	parse := &struct{ tree string }{"parsed"}

	out, err := detected(c, parse).Compile(t.Context())

	require.NoError(t, err)
	assert.Same(t, c.doc, out.Document)
	assert.Equal(t, c.diags, out.Diagnostics, "a finding at NoSource indexes no table and is kept")
	assert.Equal(t, c.doc.Sources, out.Sources, "a document's findings index its own table")
	assert.Same(t, parse, c.got.Parsed, "the compile is handed what recognizing the source parsed")
	assert.Equal(t, spec.Path, c.got.Path, "the compile is handed the source detection was")
	assert.Equal(t, spec.Data, c.got.Data)
}

// TestDetectionCompile_ADocumentMayNameWhatElseItRead pins the table's other
// end: SourceTable reads no input, so a document goes on past it to name each
// further document the compile read, and its findings may index those too.
func TestDetectionCompile_ADocumentMayNameWhatElseItRead(t *testing.T) {
	t.Parallel()
	c := keeping()
	read := ir.SourceInfo{Format: alpha.String(), Path: "shared.alpha", Hash: digest([]byte("shared"))}
	c.doc.Sources = append(c.doc.Sources, read)
	c.doc.Diagnostics = []ir.Diagnostic{at(1, ir.SeverityWarning)}
	c.diags = []ir.Diagnostic{at(1, ir.SeverityInfo)}

	out, err := detected(c, nil).Compile(t.Context())

	require.NoError(t, err)
	assert.Equal(t, c.doc.Sources, out.Sources)
	assert.Equal(t, []ir.Diagnostic{at(1, ir.SeverityWarning), at(1, ir.SeverityInfo)}, out.Diagnostics)
}

// TestDetectionCompile_BothChannelsComeBackAsOne pins that a caller reads a
// compiler's findings from one list whichever channel it filled: the
// Document's first, then each returned one it does not hold.
func TestDetectionCompile_BothChannelsComeBackAsOne(t *testing.T) {
	t.Parallel()
	stored, both, returned := at(0, ir.SeverityError), at(0, ir.SeverityWarning), at(ir.NoSource, ir.SeverityInfo)
	c := keeping()
	c.doc.Diagnostics = []ir.Diagnostic{stored, both}
	c.diags = []ir.Diagnostic{both, returned}

	out, err := detected(c, nil).Compile(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []ir.Diagnostic{stored, both, returned}, out.Diagnostics)
}

func TestDetectionCompile_ARefusalComesBackWithItsTable(t *testing.T) {
	t.Parallel()
	c := keeping()
	c.doc = nil
	c.table = []ir.SourceInfo{{Path: spec.Path}, {Path: "patch.overlay"}}
	c.diags = []ir.Diagnostic{at(1, ir.SeverityError)}

	out, err := detected(c, nil).Compile(t.Context())

	require.NoError(t, err)
	assert.Nil(t, out.Document)
	assert.Equal(t, c.table, out.Sources, "a refusal's findings index the table SourceTable names")
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
			func(c *scripted) { c.table = nil }, "does not name the source",
		},
		"a table naming another file first": {
			func(c *scripted) { c.table = []ir.SourceInfo{{Path: "other"}} }, "does not name the source",
		},
		"a refusal saying nothing": {
			func(c *scripted) { c.doc = nil }, "no error diagnostic",
		},
		"a refusal with only a warning": {
			func(c *scripted) { c.doc, c.diags = nil, []ir.Diagnostic{at(0, ir.SeverityWarning)} }, "no error diagnostic",
		},
		"a refusal finding past its table": {
			func(c *scripted) { c.doc, c.diags = nil, []ir.Diagnostic{at(1, ir.SeverityError)} }, "indexes source 1 of 1",
		},
		"a document stamped with no irVersion": {
			func(c *scripted) { c.doc.IRVersion = "" }, `stamped irVersion "", where this build writes "` + ir.IRVersion + `"`,
		},
		"a document stamped with another generation": {
			func(c *scripted) { c.doc.IRVersion = "0.0.1" }, `stamped irVersion "0.0.1"`,
		},
		"a document stamped with a newer generation": {
			func(c *scripted) { c.doc.IRVersion = "99.0.0" }, `stamped irVersion "99.0.0"`,
		},
		"a document stamped with a prerelease of this one": {
			func(c *scripted) { c.doc.IRVersion += "-rc.1" }, "-rc.1",
		},
		"a document stamped with something that is no version": {
			func(c *scripted) { c.doc.IRVersion = "latest" }, `stamped irVersion "latest"`,
		},
		"a document listing no source": {
			func(c *scripted) { c.doc.Sources = nil }, "lists 0 sources where SourceTable names 1",
		},
		"a document dropping a source the table names": {
			func(c *scripted) { c.table = append(c.table, ir.SourceInfo{Path: "patch.overlay"}) },
			"lists 1 sources where SourceTable names 2",
		},
		"a document naming a source the table names in another place": {
			func(c *scripted) {
				c.table = append(c.table, ir.SourceInfo{Path: "patch.overlay"})
				c.doc.Sources = append(c.doc.Sources, ir.SourceInfo{Path: "shared.alpha"}, ir.SourceInfo{Path: "patch.overlay"})
			},
			`source 1 is "shared.alpha" where SourceTable names "patch.overlay"`,
		},
		"a document naming the source by another path": {
			func(c *scripted) { c.doc.Sources[0].Path = "elsewhere" }, `source 0 is "elsewhere"`,
		},
		"a document reading the source as no format": {
			func(c *scripted) { c.doc.Sources[0].Format = "" }, `reads source 0 as ""`,
		},
		"a document reading the source as another format": {
			func(c *scripted) { c.doc.Sources[0].Format = "alpha@2" }, `reads source 0 as "alpha@2"`,
		},
		"a document hashing nothing": {
			func(c *scripted) { c.doc.Sources[0].Hash = "" }, `hashes source 0 as ""`,
		},
		"a document hashing other bytes": {
			func(c *scripted) { c.doc.Sources[0].Hash = digest([]byte("other")) }, "hashes source 0",
		},
		"a finding on the document past its table": {
			func(c *scripted) { c.doc.Diagnostics = []ir.Diagnostic{at(1, ir.SeverityWarning)} }, "on the document",
		},
		"a returned finding past the table": {
			func(c *scripted) { c.diags = []ir.Diagnostic{at(1, ir.SeverityWarning)} }, "indexes source 1 of 1",
		},
		"a returned finding at a sentinel the IR does not declare": {
			func(c *scripted) { c.diags = []ir.Diagnostic{at(-2, ir.SeverityWarning)} }, "indexes source -2",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := keeping()
			tc.breakIt(c)

			out, err := detected(c, nil).Compile(t.Context())

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
	c := keeping()
	c.doc = nil
	c.table = []ir.SourceInfo{{Path: spec.Path}}
	c.diags = []ir.Diagnostic{at(0, ir.SeverityError)}

	out, err := detected(c, nil).Compile(t.Context())
	require.NoError(t, err)
	c.table[0].Path = "tampered"
	c.diags[0].Code = "tampered"

	assert.Equal(t, spec.Path, out.Sources[0].Path)
	assert.Equal(t, "alpha/finding", out.Diagnostics[0].Code)
}

func TestDetectionCompile_ACompilerErrorPassesThroughUnwrapped(t *testing.T) {
	t.Parallel()
	c := keeping()
	c.err = context.Canceled

	_, err := detected(c, nil).Compile(t.Context())

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

			_, ok, err := reg.Detect(t.Context(), spec, nil)

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
				declines: []ir.Diagnostic{at(tc.source, ir.SeverityError)},
			}))

			det, _, err := reg.Detect(t.Context(), spec, nil)

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

			det, _, err := reg.Detect(t.Context(), src, nil)

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
		"an error":                  {declines: []ir.Diagnostic{at(0, ir.SeverityError)}},
		"a warning beside an error": {declines: []ir.Diagnostic{at(0, ir.SeverityWarning), at(0, ir.SeverityError)}},
		"a warning alone":           {declines: []ir.Diagnostic{at(0, ir.SeverityWarning)}, broken: true},
		"an info alone":             {declines: []ir.Diagnostic{at(ir.NoSource, ir.SeverityInfo)}, broken: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reg := compilers.NewRegistry()
			require.NoError(t, reg.Register(&stubCompiler{formats: []compilers.SourceFormat{alpha}, declines: tc.declines}))

			det, ok, err := reg.Detect(t.Context(), spec, nil)

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
