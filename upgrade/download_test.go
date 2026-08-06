package upgrade

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadFile_AtomicAndComplete(t *testing.T) {
	payload := []byte("hello-download-content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.Write(payload)
	}))
	defer srv.Close()

	SetHTTPClient(srv.Client())
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	if err := DownloadFile(ioutil.Discard, srv.URL, dest); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	// no leftover temp files
	entries, _ := ioutil.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only dest, got %d entries", len(entries))
	}
}

func TestFetchURLBytes_WithinLimit(t *testing.T) {
	payload := []byte("ok-body")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	got, err := FetchURLBytes(srv.Client(), srv.URL, 64)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestFetchURLBytes_ExceedsLimit(t *testing.T) {
	payload := []byte("0123456789abcdef")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	_, err := FetchURLBytes(srv.Client(), srv.URL, 8)
	if err == nil {
		t.Fatal("expected oversize error")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("want exceeds error, got %v", err)
	}
}

func TestFetchURLBytes_ExactLimit(t *testing.T) {
	payload := []byte("01234567") // 8 bytes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	got, err := FetchURLBytes(srv.Client(), srv.URL, int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestDownloadFile_IncompleteContentLength(t *testing.T) {
	// Advertise more bytes than we send, then close.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("short"))
		// flush then hijack-close is hard; short body with larger CL is enough for many servers.
		// httptest will still end the body after Write returns.
	}))
	defer srv.Close()

	SetHTTPClient(srv.Client())
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	err := DownloadFile(ioutil.Discard, srv.URL, dest)
	if err == nil {
		t.Fatal("expected incomplete download error")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("dest should not exist after failed download, err=%v", statErr)
	}
}
