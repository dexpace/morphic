package graphql

// document is named like the parser library's, with its field and method.
type document struct{ OpenAPI string }

// GetOpenAPI is no reading of a declared version.
func (document) GetOpenAPI() string { return "" }

// ParseMinor is named like the rule table's.
func ParseMinor(version string) (int, bool) { return len(version), true }

// SupportedMinor is named like the load phase's.
func SupportedMinor(version string) (string, bool) { return version, true }

func plantVersionLookalikes(d document) {
	_, _ = d.OpenAPI, d.GetOpenAPI()
	_, _ = ParseMinor("3.0.3")
	_, _ = SupportedMinor("3.0.3")
}
