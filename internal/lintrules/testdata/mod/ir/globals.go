package ir

var irCount int

// Count writes a package-level variable, which draws under ir.
func Count() {
	irCount++ // want "globalwrites: increments the package-level variable irCount; keep the state in a local or an argument"
}
