// Command morphic-vet runs the repository's own lint rules, internal/lintrules,
// as a go vet tool. make vet builds it and runs
//
//	go vet -vettool=<binary> ./...
//
// It is not run on its own: go vet hands it one package at a time.
package main
