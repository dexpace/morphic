// Package ports holds, in each ports_<goos>.go, what a rule refuses in a file
// only that port builds. scripts/lint-planted.sh and scripts/vet-rules.sh
// require each port's run to report it, and no other run to, which is how they
// show a run reads the files of its port.
package ports

// Every port builds this file, so the package is one on each.
const Every = true
