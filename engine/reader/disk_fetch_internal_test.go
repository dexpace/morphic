// Copyright (c) 2026 dexpace.
// Licensed under the MIT License. See LICENSE in the repository root for details.

package reader

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDisk_FetchRefusesAURLNoRequestCanBeBuiltFor pins fetch's own guard:
// locate has parsed every URL ReadDocument hands it, so only a direct call
// reaches the failure of building the request, which must fail the read
// without sending anything.
func TestDisk_FetchRefusesAURLNoRequestCanBeBuiltFor(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(srv.Close)

	got, err := Disk{Client: srv.Client()}.fetch(t.Context(), "http://[::1", 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "http://[::1")
	assert.Nil(t, got)
	assert.Zero(t, hits.Load())
}
