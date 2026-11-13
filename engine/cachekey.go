// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"maps"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/ir"
)

// runKey derives the key of a run: everything the document depends on but the
// documents it reads, which no key made before the compile can name, so an
// entry lists them instead (entryHead.Reads). FailOn is absent: the verdict is
// judged afresh. The format is too: it follows from the bytes, and finding it
// is what a hit skips. The spec's Location is present beside its Path because
// what its references name is read against it.
func (b *cacheBinding) runKey(spec compilers.Source, opts RunOptions, p plan) cache.Key {
	passes := make([]string, 0, len(p.passes))
	for _, np := range p.passes {
		passes = append(passes, np.name)
	}
	return deriveKey(keyInputs{
		Build: b.build, IRVersion: ir.IRVersion, Location: spec.Location, Path: spec.Path,
		SpecHash: ir.SourceHash(spec.Data), Passes: passes, Settings: opts.CompilerOptions,
	})
}

// keyInputs is what a run's key covers, a field per input. The fields are
// exported for the tests alone, which name each input they change.
type keyInputs struct {
	Build, IRVersion, Location, Path, SpecHash string
	Passes                                     []string
	Settings                                   map[string]string
}

// deriveKey frames in as cache.KeyOf wants: the passes after their count, then
// the settings, last and so needing none, in sorted order.
func deriveKey(in keyInputs) cache.Key {
	const leading = 6 // the fields before the passes
	fields := make([]string, 0, leading+len(in.Passes)+2*len(in.Settings))
	fields = append(fields, in.Build, in.IRVersion, in.Location, in.Path, in.SpecHash, strconv.Itoa(len(in.Passes)))
	fields = append(fields, in.Passes...)
	for _, name := range slices.Sorted(maps.Keys(in.Settings)) {
		fields = append(fields, name, in.Settings[name])
	}
	return cache.KeyOf(fields...)
}
