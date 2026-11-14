package graphql

// Written is written by the package's external test: a write in a test file
// draws nothing.
var (
	graphqlCount int
	Written      int
)

// Count writes a package-level variable, which draws under compilers.
func Count() {
	graphqlCount++ // want "globalwrites: increments the package-level variable graphqlCount; keep the state in a local or an argument"
}
