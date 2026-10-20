// Package compile is named like the framework's package without being it.
package compile

// SubHint has the framework's name and none of it.
func SubHint(parent, suffix string) string { return parent + suffix }

// Space has the framework's Space name and nothing else of it.
type Space string
