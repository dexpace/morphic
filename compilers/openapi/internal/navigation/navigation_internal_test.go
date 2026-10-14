package navigation

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeyed_CostsNoAllocation holds keyed, which runs as pointer reads step
// through models, at no allocation. Ranging over reflect.Type.Fields costs it
// four a call, and go fix and modernize both propose exactly that rewrite.
// .golangci.yml declines it for this file; go fix would apply it, and this test
// is what catches that. It is not run in parallel: AllocsPerRun measures the
// process.
func TestKeyed_CostsNoAllocation(t *testing.T) {
	type model struct {
		Tagged   int `key:"tagged"`
		Untagged int
	}
	rt := reflect.TypeFor[model]()
	require.True(t, keyed(rt, "tagged", false), "a tagged field answers its tag")
	require.True(t, keyed(rt, "Untagged", true), "by name, an untagged field answers its name")
	require.False(t, keyed(rt, "Untagged", false), "by tag alone, it answers nothing")

	assert.Zero(t, testing.AllocsPerRun(10, func() { _ = keyed(rt, "absent", true) }),
		"reading every field and finding no answer allocates nothing")
}
