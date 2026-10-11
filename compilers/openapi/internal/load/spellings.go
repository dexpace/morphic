package load

import (
	"encoding/json/jsontext"
	"maps"
	"slices"
	"sync"
)

// heldSpellings records what the source is held under besides its own keys:
// each spelling of its file name a $ref was found to use, and the pointers of
// the objects held under it. A second resolution, which reads the documents
// the first prepared without preparing them again, holds the same from it.
// Safe for concurrent use, as the readers sharing it are.
type heldSpellings struct {
	mu    sync.Mutex
	sites map[string]map[jsontext.Pointer]struct{}
}

// newHeldSpellings returns a record holding nothing.
func newHeldSpellings() *heldSpellings {
	return &heldSpellings{sites: map[string]map[jsontext.Pointer]struct{}{}}
}

// note records that the source is held under key and, when site is not empty,
// that the object at site is. It reports whether site was not recorded under
// key already, so an object is stored once for each pair, however many $refs
// write it.
func (h *heldSpellings) note(key string, site jsontext.Pointer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	held, ok := h.sites[key]
	if !ok {
		held = map[jsontext.Pointer]struct{}{}
		h.sites[key] = held
	}
	if site == "" {
		return false
	}
	if _, dup := held[site]; dup {
		return false
	}
	held[site] = struct{}{}
	return true
}

// keys returns, sorted, each spelling recorded.
func (h *heldSpellings) keys() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Sorted(maps.Keys(h.sites))
}

// at returns, sorted, the pointers of the objects recorded under key.
func (h *heldSpellings) at(key string) []jsontext.Pointer {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Sorted(maps.Keys(h.sites[key]))
}
