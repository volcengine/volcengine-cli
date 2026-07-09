package upgrade

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
