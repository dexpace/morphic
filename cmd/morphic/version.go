package main

import (
	"runtime/debug"

	"github.com/dexpace/morphic/ir"
)

// versionTemplate is what --version prints: the build, then the IR schema it
// writes, which is what a consumer storing the output has to pin. The schema's
// version is a constant of the build, so it is spliced in here rather than
// looked up per run.
const versionTemplate = "{{.DisplayName}} version {{.Version}}\nIR schema " + ir.IRVersion + "\n"

// devVersion names a build that carries no module version.
const devVersion = "devel"

// moduleVersion returns the version the Go toolchain stamped into the binary: a
// release tag, or the pseudo-version of the commit it was built from, with
// "+dirty" when the tree had changes. fang reads the same build info but only
// trusts a module checksum, so it prints "unknown" for every build from a
// checkout — which is where most morphic binaries come from.
func moduleVersion(info *debug.BuildInfo) string {
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return devVersion
	}
	return info.Main.Version
}
