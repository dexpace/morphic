// Package emitter stands for a package under emitters, none of which is built:
// the rule holds it as it holds ir, pass and the compilers.
package emitter

var emitted = map[string]int{}

// Emit writes a package-level variable, which draws under emitters.
func Emit() {
	emitted["a"] = 1 // want "globalwrites: writes an element or field of the package-level variable emitted; keep the state in a local or an argument"
}
