// Package barrier runs code its caller does not own and turns a panic in it
// into an error; Line quotes what that code says on one bounded line. A
// compiler or a pass that panics breaks its contract; Catch reports the breach
// where the caller can name the stage. Outside tests, the lint configuration
// lets no other package recover a panic.
//
// Catch recovers the calling goroutine only. A fatal error (stack exhaustion, a
// concurrent map write), a panic on a goroutine the code started, and a call
// that never returns are not its to catch; a context bounds the last.
package barrier
