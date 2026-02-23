package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIServer_ListenAndServe_DefaultAddr(t *testing.T) {
	srv, _ := testServer(t)
	httpSrv := srv.ListenAndServe("")
	require.NotNil(t, httpSrv)
	assert.Equal(t, ":8080", httpSrv.Addr)
}

func TestAPIServer_ListenAndServe_CustomAddr(t *testing.T) {
	srv, _ := testServer(t)
	httpSrv := srv.ListenAndServe(":9090")
	require.NotNil(t, httpSrv)
	assert.Equal(t, ":9090", httpSrv.Addr)
}

func TestAPIServer_Hub(t *testing.T) {
	srv, _ := testServer(t)
	hub := srv.Hub()
	assert.NotNil(t, hub)
	assert.Equal(t, srv.wsHub, hub)
}

func TestAPIServer_Handler(t *testing.T) {
	srv, _ := testServer(t)
	h := srv.Handler()
	assert.NotNil(t, h)
}

func TestAPIServer_Shutdown(t *testing.T) {
	srv, _ := testServer(t)
	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestAPIServer_Version(t *testing.T) {
	srv, _ := testServer(t)
	assert.Equal(t, "1.0.0", srv.version)
}
