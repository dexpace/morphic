package compilers_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dexpace/morphic/barrier"
	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/ir"
)

// breaker is a compiler that panics in the method at names, with value, and
// counts how often it is asked each. Its other methods answer as scripted does.
type breaker struct {
	*scripted
	at    string
	value any
	asked map[string]int
}

func newBreaker(at string, value any) *breaker {
	s := keeping()
	s.marker = "alpha"
	return &breaker{scripted: s, at: at, value: value, asked: map[string]int{}}
}

// enter counts a call of method and panics if it is the one to break.
func (b *breaker) enter(method string) {
	b.asked[method]++
	if method == b.at {
		panic(b.value)
	}
}

func (b *breaker) Formats() []compilers.SourceFormat {
	b.enter("Formats")
	return b.scripted.Formats()
}

func (b *breaker) Detect(ctx context.Context, src compilers.Source, opts compilers.Options) (compilers.Recognition, []ir.Diagnostic, bool) {
	b.enter("Detect")
	return b.scripted.Detect(ctx, src, opts)
}

func (b *breaker) DecodeOptions(set compilers.OptionSet) (any, error) {
	b.enter("DecodeOptions")
	return b.scripted.DecodeOptions(set)
}

func (b *breaker) Compile(ctx context.Context, sources []compilers.Source, opts compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	b.enter("Compile")
	return b.scripted.Compile(ctx, sources, opts)
}

func (b *breaker) SourceLimit(opts compilers.Options) int {
	b.enter("SourceLimit")
	return b.scripted.SourceLimit(opts)
}

func (b *breaker) SourceTable(sources []compilers.Source, opts compilers.Options) []ir.SourceInfo {
	b.enter("SourceTable")
	return b.scripted.SourceTable(sources, opts)
}

// configureViaDecodeOptions is the Configure the engine gives the registry: it
// asks the compiler to decode its options, so a DecodeOptions that panics
// panics inside configuring.
func configureViaDecodeOptions(c compilers.Compiler) (compilers.Options, error) {
	decoded, err := c.DecodeOptions(compilers.OptionSet{})
	return compilers.Options{FormatOptions: decoded}, err
}

// requirePanic requires err to be a contract breach recording a panic of value.
func requirePanic(t *testing.T, err error, value any) {
	t.Helper()
	require.ErrorIs(t, err, compilers.ErrContract)
	perr, ok := errors.AsType[*barrier.PanicError](err)
	require.True(t, ok, "the breach should carry the panic: %v", err)
	assert.Equal(t, value, perr.Value())
	assert.NotContains(t, err.Error(), "\n", "a breach is one line")
}

func TestRegistry_RegisterRefusesACompilerWhoseFormatsPanic(t *testing.T) {
	t.Parallel()
	reg := compilers.NewRegistry()

	err := reg.Register(newBreaker("Formats", "formats exploded"))

	requirePanic(t, err, "formats exploded")
	assert.Contains(t, err.Error(), "compilers: register: Formats: panic: formats exploded")
	assert.Empty(t, reg.Formats(), "on failure nothing is registered")
}

// TestRegistry_DetectRefusesACompilerThatPanics requires a panic in each call
// Detect makes of a compiler to be a breach naming the compiler and the call,
// whether the compiler is the one that takes the source or one asked on the way
// to it.
func TestRegistry_DetectRefusesACompilerThatPanics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		at        string
		configure compilers.Configure
		wantCall  string
	}{
		{"Detect", "Detect", nil, "detect by alpha@1: Detect: panic: boom"},
		{"configuring through DecodeOptions", "DecodeOptions", configureViaDecodeOptions, "detect by alpha@1: configure: panic: boom"},
		{"SourceLimit", "SourceLimit", nil, "source limit of alpha@1: SourceLimit: panic: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := newBreaker(tt.at, "boom")

			det, ok, err := registryOf(t, b).Detect(t.Context(), spec, tt.configure)

			requirePanic(t, err, "boom")
			assert.Contains(t, err.Error(), tt.wantCall)
			assert.False(t, ok)
			assert.Equal(t, compilers.Detection{}, det)
		})
	}
}

// TestRegistry_DetectRefusesAPanicBeforeALaterCompilerTakesTheSource pins that
// a compiler asked on the way to the one that takes the source is held to the
// contract too: it is the compiler's bug whoever would have taken the source.
func TestRegistry_DetectRefusesAPanicBeforeALaterCompilerTakesTheSource(t *testing.T) {
	t.Parallel()
	first := newBreaker("DecodeOptions", "first exploded")
	second := &stubCompiler{formats: []compilers.SourceFormat{beta}, marker: "alpha"}

	_, ok, err := registryOf(t, first, second).Detect(t.Context(), spec, configureViaDecodeOptions)

	requirePanic(t, err, "first exploded")
	assert.False(t, ok)
	assert.Empty(t, second.saw, "the later compiler is not asked once an earlier one breaks")
}

// TestRegistry_DetectDoesNotAskACompilerAfterTheOneThatTakesTheSource pins that
// a compiler registered after the claimant is never called, so it cannot break
// a source it has no part in.
func TestRegistry_DetectDoesNotAskACompilerAfterTheOneThatTakesTheSource(t *testing.T) {
	t.Parallel()
	taker := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha"}
	later := newBreaker("Detect", "never reached")
	later.formats = []compilers.SourceFormat{beta}

	det, ok, err := registryOf(t, taker, later).Detect(t.Context(), spec, nil)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Same(t, compilers.Compiler(taker), det.Compiler)
	assert.Empty(t, later.asked["Detect"])
}

// TestRegistry_DetectRefusesAnOwnersConfigureThatPanics pins the second place
// configuring is asked: the owner of a format another compiler recognized, which
// is configured in its own right once the claim is known.
func TestRegistry_DetectRefusesAnOwnersConfigureThatPanics(t *testing.T) {
	t.Parallel()
	recognizer := &stubCompiler{formats: []compilers.SourceFormat{alpha}, marker: "alpha", detects: beta}
	owner := newBreaker("DecodeOptions", "owner exploded")
	owner.formats = []compilers.SourceFormat{beta}

	_, ok, err := registryOf(t, recognizer, owner).Detect(t.Context(), spec, configureViaDecodeOptions)

	requirePanic(t, err, "owner exploded")
	assert.Contains(t, err.Error(), "detect by beta@2: configure: panic")
	assert.False(t, ok)
}

// TestRegistry_FormatsAreAskedOnce pins that the registry keeps the answer
// Formats gave at registration: it is not asked again by detection or a
// compile, so a compiler that changes or breaks it afterwards cannot make the
// registry disagree with itself, and the copy it keeps is its own.
func TestRegistry_FormatsAreAskedOnce(t *testing.T) {
	t.Parallel()
	b := newBreaker("none", nil)
	b.detects = alpha // what its Detect names, whatever it later says it serves
	reg := registryOf(t, b)
	// Whatever the compiler does to the slice it answered with, or to its answer
	// later, the registry holds what it was told at registration.
	b.formats[0] = beta
	b.at = "Formats"

	det, ok, err := reg.Detect(t.Context(), spec, nil)
	require.NoError(t, err, "Formats is not asked again, so its panic is never reached")
	require.True(t, ok)
	_, compileErr := det.Compile(t.Context())

	require.NoError(t, compileErr)
	assert.Equal(t, 1, b.asked["Formats"], "asked at registration alone")
	assert.Equal(t, alpha, det.Recognition.Format)
	assert.Same(t, compilers.Compiler(b), det.Compiler, "the format it registered under is its own")
	assert.Equal(t, []compilers.SourceFormat{alpha}, reg.Formats())
}

// TestRegistry_DetectNamesTheFormatsItRegisteredNotTheOnesItIsToldNow pins the
// same snapshot where it is visible: a breach names the compiler by what it
// reported when it registered.
func TestRegistry_DetectNamesTheFormatsItRegisteredNotTheOnesItIsToldNow(t *testing.T) {
	t.Parallel()
	b := newBreaker("none", nil)
	b.declines = []ir.Diagnostic{at(0, ir.SeverityWarning)} // a decline with no error is a breach
	reg := registryOf(t, b)
	b.formats[0] = beta

	_, _, err := reg.Detect(t.Context(), compilers.Source{Path: spec.Path, Data: []byte("unmarked")}, nil)

	require.ErrorIs(t, err, compilers.ErrContract)
	assert.Contains(t, err.Error(), "detect by alpha@1")
	assert.NotContains(t, err.Error(), "beta@2")
}

// TestDetectionCompile_APanicIsAContractError requires a panic in either method
// a compile calls to come back as a breach, whatever the panic value is, with
// nothing of the compile handed on.
func TestDetectionCompile_APanicIsAContractError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("sentinel")
	tests := []struct {
		name   string
		at     string
		value  any
		method string
	}{
		{"Compile with a string", "Compile", "boom", "Compile"},
		{"Compile with an error", "Compile", sentinel, "Compile"},
		{"Compile with a number", "Compile", 42, "Compile"},
		{"Compile with a wrapped error", "Compile", fmt.Errorf("outer: %w", sentinel), "Compile"},
		{"SourceTable", "SourceTable", "table exploded", "SourceTable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := newBreaker(tt.at, tt.value)

			out, err := detected(b, nil).Compile(t.Context())

			requirePanic(t, err, tt.value)
			assert.Contains(t, err.Error(), "compilers: compile as alpha@1: "+tt.method+": panic: ")
			assert.Equal(t, compilers.Compiled{}, out, "nothing of a broken compile comes back")
			if asError, ok := tt.value.(error); ok {
				assert.ErrorIs(t, err, asError, "an error the compiler panicked with stays reachable")
			}
		})
	}
}

func TestDetectionCompile_ARuntimePanicIsAContractError(t *testing.T) {
	t.Parallel()
	c := &runtimePanicker{}

	_, err := detected(c, nil).Compile(t.Context())

	require.ErrorIs(t, err, compilers.ErrContract)
	var rerr runtime.Error
	assert.ErrorAs(t, err, &rerr, "the runtime's error stays reachable")
}

// runtimePanicker dereferences nil in Compile.
type runtimePanicker struct{ stubCompiler }

func (*runtimePanicker) Compile(context.Context, []compilers.Source, compilers.Options) (*ir.Document, []ir.Diagnostic, error) {
	var doc *ir.Document
	return &ir.Document{Name: doc.Name}, nil, nil
}

func (*runtimePanicker) SourceTable([]compilers.Source, compilers.Options) []ir.SourceInfo {
	return []ir.SourceInfo{{Path: spec.Path}}
}

// TestDetectionCompile_ACompilePanicSkipsTheSourceTable pins the order: the
// table is read after the compile, so a compile that broke is reported as the
// breach it is and not as whatever the table would have said.
func TestDetectionCompile_ACompilePanicSkipsTheSourceTable(t *testing.T) {
	t.Parallel()
	b := newBreaker("Compile", "boom")

	_, err := detected(b, nil).Compile(t.Context())

	require.ErrorIs(t, err, compilers.ErrContract)
	assert.Zero(t, b.asked["SourceTable"])
}

// TestDetectionCompile_ARegistryThatBrokeOnceStillWorks pins that a breach is
// the compile's and not the registry's: the same registry takes the next
// source.
func TestDetectionCompile_ARegistryThatBrokeOnceStillWorks(t *testing.T) {
	t.Parallel()
	b := newBreaker("Compile", "boom")
	reg := registryOf(t, b)
	det, ok, err := reg.Detect(t.Context(), spec, nil)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = det.Compile(t.Context())
	require.ErrorIs(t, err, compilers.ErrContract)

	b.at = "none"
	det, ok, err = reg.Detect(t.Context(), spec, nil)
	require.NoError(t, err)
	require.True(t, ok)
	out, err := det.Compile(t.Context())

	require.NoError(t, err)
	assert.NotNil(t, out.Document)
}
