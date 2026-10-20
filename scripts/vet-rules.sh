#!/usr/bin/env bash
#
# vet-rules.sh runs the repository's own lint rules, internal/lintrules, over the
# module through go vet and cmd/morphic-vet, on the host and then on each port it
# is given as GOOS/GOARCH. A file one port alone builds is read by that port's
# run only, so the rules reach it as the build does. Each run must find the
# module clean, and must report the registry the file under
# internal/lintrules/testdata/ports for its GOOS builds, if there is one, which
# shows the run reads its port's files.
#
# go vet caches each package's result under the tool's build ID, so an edit to a
# rule reruns every package, and an unchanged tree costs little.
#
# Usage: vet-rules.sh [GOOS/GOARCH ...]        (make vet passes $(PORTS))
set -euo pipefail

cd "$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
ports=internal/lintrules/testdata/ports

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
tool="$work/morphic-vet"
go build -o "$tool" ./cmd/morphic-vet

# Each run vets the module and the ports package in one go: their findings, the
# file each is in, must be exactly the ports file of the run's GOOS, if there is
# one. A finding in the module, or an error, names a file of its own.
for port in "$(go env GOOS)/$(go env GOARCH)" "$@"; do
	if [[ "$port" != */* ]]; then
		echo "vet-rules: $port is no GOOS/GOARCH pair" >&2
		exit 2
	fi
	goos="${port%/*}"
	want=""
	if [[ -f "$ports/ports_$goos.go" ]]; then
		want="$ports/ports_$goos.go"
	fi

	status=0
	GOOS="$goos" GOARCH="${port#*/}" go vet -vettool="$tool" ./... "./$ports" 2>"$work/vet.out" || status=$?
	got="$(sed -nE 's/^([^#][^:]*):[0-9]+:[0-9]+: .*/\1/p' "$work/vet.out")"
	if [[ "$got" != "$want" || "$status" -ne "$((${#want} > 0))" ]]; then
		echo "vet-rules: on $port, the one finding must be ${want:-none} (exit $status):" >&2
		cat "$work/vet.out" >&2
		exit 1
	fi
	echo "vet-rules: on $port, the module is clean${want:+, and $want draws its finding}"
done
