// Package barrier runs code its caller does not own and turns a panic in it
// into an error. A compiler or a pass is held to a contract, and one that
// panics has broken it; Catch reports the breach where the caller can name the
// stage, instead of unwinding past it.
//
// It recovers the calling goroutine only. A fatal error (stack exhaustion, a
// concurrent map write), a panic on a goroutine the code started, and a call
// that never returns are not its to catch; a context bounds the last.
package barrier
