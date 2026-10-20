// Package analysis inventories Go functions and their direct calls in one module.
package analysis

import "crypto/sha256"

// ID identifies a declaration independently of its absolute checkout location.
type ID string

// Function describes a declaration or function literal in a module snapshot.
type Function struct {
	ID      ID
	Name    string
	Package string
	File    string
	Line    int
	// Fingerprint covers the signature and body, ignoring comments and formatting.
	Fingerprint [sha256.Size]byte
	// DynamicCalls counts calls whose target cannot be resolved statically.
	DynamicCalls int
}

// Call is a direct calling relationship, collapsed across repeated call sites.
type Call struct {
	Caller ID
	Callee ID
}

// Snapshot contains project functions and calls for one active build configuration.
type Snapshot struct {
	Functions map[ID]Function
	Calls     []Call
}
