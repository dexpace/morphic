package engine_test

import (
	"context"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine"
	"github.com/dexpace/morphic/internal/testspec"
	"github.com/dexpace/morphic/ir"
)

// rereadCompiler reads its spec again, through the reader it is handed, and
// names its document by what that read returned, so a test sees which bytes a
// run serves for the spec's own path. Its "read" setting names a file, which
// the compile reads through the same reader as a setting's file is read, and
// whose bytes become the document's Version. before, when set, runs ahead of
// the reads, and decoding ahead of decoding the settings; at, when set, is
// where the spec is read again, in place of its path.
type rereadCompiler struct {
	stubFront
	before   func(path string)
	decoding func()
	at       string
}

func (c rereadCompiler) DecodeOptions(set compilers.OptionSet) (any, error) {
	if c.decoding != nil {
		c.decoding()
	}
	return set.Settings["read"], nil
}

func (c rereadCompiler) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	if c.before != nil {
		c.before(sources[0].Path)
	}
	location := sources[0].Path
	if c.at != "" {
		location = c.at
	}
	data, err := opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{Location: location})
	if err != nil {
		return nil, nil, err
	}
	setting, err := readSetting(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	return &ir.Document{IRVersion: ir.IRVersion, Name: string(data), Version: setting, Types: ir.TypeRegistry{},
		Sources: sourcesOf("openapi@3.1", sources)}, nil, nil
}

// readSetting reads the file the "read" setting names, which DecodeOptions
// handed on as the options, through the compile's reader: "" when none is
// named.
func readSetting(ctx context.Context, opts compilers.Options) (string, error) {
	name, _ := opts.FormatOptions.(string)
	if name == "" {
		return "", nil
	}
	data, err := opts.Reader.ReadDocument(ctx, compilers.DocumentRequest{Location: name})
	return string(data), err
}

// TestEngine_RunSpecCompilesTheHeldBytesBesideFilesOnDisk pins RunSpec's two
// halves: the spec is the bytes held, though a file of other content sits at
// its path, and the documents it references are read from disk beside it.
func TestEngine_RunSpecCompilesTheHeldBytesBesideFilesOnDisk(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	root := writeNamed(t, "root.yaml", testspec.Tiny)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(root), "common.yaml"), []byte(splitCommon), 0o644))

	res, err := eng.RunSpec(t.Context(), compilers.Source{Path: root, Data: []byte(splitRoot)}, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document, "diagnostics: %+v", res.Diagnostics)
	assert.Equal(t, "Split", res.Document.Name, "the held bytes are compiled, not the file")
	assert.Equal(t, ir.SourceHash([]byte(splitRoot)), res.Sources[0].Hash)
	assert.Empty(t, unresolved(res.Diagnostics), "both references are read from disk")
}

// TestEngine_RunSpecServesTheHeldSpecWhereverItsPathIsRead pins that the held
// bytes stand for the file through the whole run: a compiler reading the
// spec's path again is served them, and so is a setting naming that path, dot
// segments or none, while a setting naming another file reads it from disk.
func TestEngine_RunSpecServesTheHeldSpecWhereverItsPathIsRead(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(rereadCompiler{}))
	require.NoError(t, err)
	spec := writeNamed(t, "spec.yaml", "on disk")
	dir, sep := filepath.Dir(spec), string(filepath.Separator)
	other := filepath.Join(dir, "other.yaml")
	require.NoError(t, os.WriteFile(other, []byte("other"), 0o644))

	for _, tc := range []struct{ name, setting, want string }{
		{"a setting naming the spec", spec, "held"},
		{"a setting naming the spec with dot segments", dir + sep + "." + sep + "spec.yaml", "held"},
		{"a setting naming another file", other, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, err := eng.RunSpec(t.Context(), compilers.Source{Path: spec, Data: []byte("held")},
				engine.RunOptions{CompilerOptions: map[string]string{"read": tc.setting}})

			require.NoError(t, err)
			require.NotNil(t, res.Document)
			assert.Equal(t, "held", res.Document.Name, "the compiler's read of the spec's path")
			assert.Equal(t, tc.want, res.Document.Version, "the setting's read")
		})
	}
}

// TestEngine_RunSpecNamesTheHeldSpecLexically pins that the held bytes stand
// for spec.Path as it is spelt, as reader.Memory names a document: another
// spelling of the same file, here the path a symbolic link leads to, names
// another document, which is read from disk.
func TestEngine_RunSpecNamesTheHeldSpecLexically(t *testing.T) {
	t.Parallel()
	spec := writeNamed(t, "spec.yaml", "on disk")
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(filepath.Dir(spec), link))
	// Every link resolved, the temporary directory's own among them, so the
	// read names the file as no other spelling does.
	resolved, err := filepath.EvalSymlinks(spec)
	require.NoError(t, err)
	eng, err := engine.New(engine.WithCompilers(rereadCompiler{at: resolved}))
	require.NoError(t, err)

	res, err := eng.RunSpec(t.Context(),
		compilers.Source{Path: filepath.Join(link, "spec.yaml"), Data: []byte("held")}, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "on disk", res.Document.Name)
}

// TestEngine_RunReadsTheSpecOnce pins that Run holds the bytes it read: a
// compiler reading the spec's path again is served them, though the file
// changed after the run read it.
func TestEngine_RunReadsTheSpecOnce(t *testing.T) {
	t.Parallel()
	rewrite := func(path string) { assert.NoError(t, os.WriteFile(path, []byte("second"), 0o644)) }
	eng, err := engine.New(engine.WithCompilers(rereadCompiler{before: rewrite}))
	require.NoError(t, err)
	spec := writeNamed(t, "spec.yaml", "first")

	res, err := eng.Run(t.Context(), spec, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "first", res.Document.Name)
	data, err := os.ReadFile(spec)
	require.NoError(t, err)
	require.Equal(t, "second", string(data), "the file changed under the run, or this compares nothing")
}

// TestEngine_RunSpecAtAPathNoRequestCanName pins a spec whose path reads as an
// opaque URI, which no reference can name: it compiles from the bytes held,
// and a setting naming another file is still read from disk.
func TestEngine_RunSpecAtAPathNoRequestCanName(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	const overlay = "overlay: 1.0.0\ninfo: {title: o, version: \"1\"}\n" +
		"actions:\n  - target: $.info\n    update: {title: Patched}\n"
	patch := writeNamed(t, "patch.yaml", overlay)

	res, err := eng.RunSpec(t.Context(), compilers.Source{Path: "file:held.yaml", Data: []byte(testspec.Tiny)},
		engine.RunOptions{CompilerOptions: map[string]string{"overlay": patch}})

	require.NoError(t, err)
	require.NotNil(t, res.Document, "diagnostics: %+v", res.Diagnostics)
	assert.Equal(t, "Patched", res.Document.Name)
	assert.Equal(t, "file:held.yaml", res.Sources[0].Path)
}

// settingCompiler names its document by what its "read" setting read, and
// reads nothing else, so a spec no request can name compiles.
type settingCompiler struct{ rereadCompiler }

func (settingCompiler) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	setting, err := readSetting(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	return &ir.Document{IRVersion: ir.IRVersion, Name: setting, Types: ir.TypeRegistry{},
		Sources: sourcesOf("openapi@3.1", sources)}, nil, nil
}

// TestEngine_RunSpecReadsASettingAsAReferenceIsRead pins that a file a setting
// names is read as a document a reference names is: by the one reader, so a
// file URI naming the held spec is served it, and a spec held at a path no
// request can name is no document a setting naming that path reads either,
// which is refused as a reference spelling it is.
func TestEngine_RunSpecReadsASettingAsAReferenceIsRead(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(settingCompiler{}))
	require.NoError(t, err)
	spec := writeNamed(t, "spec.yaml", "on disk")
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(spec)}).String()

	res, err := eng.RunSpec(t.Context(), compilers.Source{Path: spec, Data: []byte("held")},
		engine.RunOptions{CompilerOptions: map[string]string{"read": uri}})
	require.NoError(t, err)
	require.NotNil(t, res.Document)
	assert.Equal(t, "held", res.Document.Name, "a file URI naming the spec names the held spec")

	res, err = eng.RunSpec(t.Context(), compilers.Source{Path: "file:held.yaml", Data: []byte("held")},
		engine.RunOptions{CompilerOptions: map[string]string{"read": "file:held.yaml"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no absolute path")
	assert.Nil(t, res)
}

// TestEngine_RunSpecCancelledWhileDecodingFailsAsCancelled pins that a run
// cancelled while its settings are decoded fails as cancelled, never as the
// missing file an unsaved buffer has, and reads nothing at the spec's path.
func TestEngine_RunSpecCancelledWhileDecodingFailsAsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	eng, err := engine.New(engine.WithCompilers(rereadCompiler{decoding: cancel}))
	require.NoError(t, err)
	unsaved := filepath.Join(t.TempDir(), "unsaved.yaml")

	res, err := eng.RunSpec(ctx, compilers.Source{Path: unsaved, Data: []byte("held")},
		engine.RunOptions{CompilerOptions: map[string]string{"read": unsaved}})

	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, fs.ErrNotExist)
	assert.Nil(t, res)
}

// TestEngine_RunSpecAgreesWithRun pins that a spec held in memory is the spec
// read from disk: the same bytes at the same path, handed once as a path and
// once held, give one Result.
func TestEngine_RunSpecAgreesWithRun(t *testing.T) {
	t.Parallel()
	eng, err := engine.New()
	require.NoError(t, err)
	root := writeNamed(t, "root.yaml", splitRoot)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(root), "common.yaml"), []byte(splitCommon), 0o644))

	fromDisk, err := eng.Run(t.Context(), root, engine.RunOptions{})
	require.NoError(t, err)
	held, err := eng.RunSpec(t.Context(), compilers.Source{Path: root, Data: []byte(splitRoot)}, engine.RunOptions{})
	require.NoError(t, err)

	require.Empty(t, unresolved(fromDisk.Diagnostics), "the disk run must resolve both, or this compares nothing")
	assert.Empty(t, cmp.Diff(fromDisk, held))
}

// parsedWary claims only a source carrying no Parsed, so a run handing a
// caller's Parsed on to detection finds no compiler for the spec.
type parsedWary struct{ rereadCompiler }

func (parsedWary) Detect(ctx context.Context, src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	if src.Parsed != nil {
		return compilers.Recognition{}, nil, false
	}
	return stubFront{}.Detect(ctx, src, opts)
}

// TestEngine_RunSpecIgnoresParsed pins that the spec's Parsed is not read: it
// is one compile's live state, so a caller's value, here one no compiler
// knows, is dropped before any compiler is asked to recognize the spec.
func TestEngine_RunSpecIgnoresParsed(t *testing.T) {
	t.Parallel()
	eng, err := engine.New(engine.WithCompilers(parsedWary{}))
	require.NoError(t, err)

	res, err := eng.RunSpec(t.Context(),
		compilers.Source{Path: "spec.yaml", Data: []byte("held"), Parsed: struct{}{}}, engine.RunOptions{})

	require.NoError(t, err)
	require.NotNil(t, res.Document, "diagnostics: %+v", res.Diagnostics)
	assert.Equal(t, "held", res.Document.Name)
}

// TestEngine_RunSpecRefusals pins that a misconfigured run is a Go error
// naming its fault, before anything is compiled: a spec with no path, an
// option RunOptions refuses, and an engine no constructor built.
func TestEngine_RunSpecRefusals(t *testing.T) {
	t.Parallel()
	built, err := engine.New()
	require.NoError(t, err)
	tiny := compilers.Source{Path: "s.yaml", Data: []byte(testspec.Tiny)}
	tests := []struct {
		name string
		eng  *engine.Engine
		spec compilers.Source
		opts engine.RunOptions
		want string
	}{
		{"a spec with no path", built, compilers.Source{Data: tiny.Data}, engine.RunOptions{}, "no Path"},
		{"a pass naming none", built, tiny, engine.RunOptions{DisablePasses: []string{"nope"}}, "unknown pass"},
		{"a threshold naming none", built, tiny, engine.RunOptions{FailOn: "fatal"}, "unknown fail-on threshold"},
		{"both option channels", built, tiny, engine.RunOptions{FormatOptions: struct{}{},
			CompilerOptions: map[string]string{"k": "v"}}, "not both"},
		{"an unbuilt engine", &engine.Engine{}, tiny, engine.RunOptions{}, "uninitialized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := tt.eng.RunSpec(t.Context(), tt.spec, tt.opts)

			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}
