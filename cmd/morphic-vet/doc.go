// Command morphic-vet runs the repository's own lint rules, internal/lintrules,
// as a go vet tool. make vet builds it and runs
//
//	go vet -vettool=<binary> ./...
//
// on the host and on every port make build covers, so a file one port alone
// builds is read by that port's run. It is not run on its own: go vet hands it
// one package at a time.
package main
