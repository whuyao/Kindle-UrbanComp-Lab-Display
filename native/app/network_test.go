package main

import (
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCycleClientReusesTLSAndReleasesIdleConnections(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.RemoteAddr) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := client(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	request := func() string {
		t.Helper()
		r, err := c.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	first := request()
	if next := request(); next != first {
		t.Fatal("repeated TLS connection", first, next)
	}
	c.CloseIdleConnections()
	if next := request(); next == first {
		t.Fatal("idle connection not released")
	}
	// Keep normal certificate verification; reuse must not bypass trust checks.
	untrusted, err := client("../package/urbancomp-ink/cacert.pem")
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.CloseIdleConnections()
	if response, err := untrusted.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
}
