// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package engine

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/dexpace/morphic/compilers"
	"github.com/dexpace/morphic/engine/cache"
	"github.com/dexpace/morphic/ir"
)

// keyOf derives the key of a run: everything the document depends on but the
// documents it reads, which no key made before the compile can name, so an
// entry lists them instead (entryHead.Reads). FailOn is absent: the verdict is
// judged afresh. The format is too: it follows from the bytes, and finding it
// is what a hit skips. The working directory is present because a compile reads
// it, to relate a relative path to an absolute one, and so reaches type IDs
// (#1297).
func (b *cacheBinding) keyOf(spec compilers.Source, opts RunOptions, p plan) (cache.Key, error) {
	dir, err := b.workingDir()
	if err != nil {
		return cache.Key{}, fmt.Errorf("engine: cache: %w", err)
	}
	passes := make([]string, 0, len(p.passes))
	for _, np := range p.passes {
		passes = append(passes, np.name)
	}
	return keyFor(keyInputs{
		build: b.build, irVersion: ir.IRVersion, dir: dir, path: spec.Path, specHash: ir.SourceHash(spec.Data),
		passes: passes, settings: opts.CompilerOptions,
	}), nil
}

// keyInputs is what a run's key covers, a field per input.
type keyInputs struct {
	build, irVersion, dir, path, specHash string
	passes                                []string
	settings                              map[string]string
}

// keyFor frames in as cache.KeyOf wants: the passes after their count, then the
// settings, last and so needing none, in sorted order.
func keyFor(in keyInputs) cache.Key {
	const leading = 6 // the fields before the passes
	fields := make([]string, 0, leading+len(in.passes)+2*len(in.settings))
	fields = append(fields, in.build, in.irVersion, in.dir, in.path, in.specHash, strconv.Itoa(len(in.passes)))
	fields = append(fields, in.passes...)
	for _, name := range slices.Sorted(maps.Keys(in.settings)) {
		fields = append(fields, name, in.settings[name])
	}
	return cache.KeyOf(fields...)
}
