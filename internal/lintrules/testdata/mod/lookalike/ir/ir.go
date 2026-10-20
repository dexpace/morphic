// Package ir is named like the IR's package and declares its names, without
// being it.
package ir

// TypeRegistry has the registry's name and nothing else of it.
type TypeRegistry map[string]int

// OpID has the IR's OpID name.
type OpID string

// Naming has the IR's Naming name and its name channels.
type Naming struct{ Canonical, Hint string }
