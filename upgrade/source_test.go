package upgrade

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveAssetSource_FallsBackWhenCDNChecksumIsMissing(t *testing.T) {
	version := "1.2.3"
	archive := ArchiveName(version, runtime.GOOS, runtime.GOARCH)
	checksum := ChecksumName(version)

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v" + version + "/" + archive:
			w.WriteHeader(http.StatusOK)
		case "/v" + version + "/" + checksum:
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cdn.Close()

	oldDownloadBase := os.Getenv(EnvDownloadBaseURL)
	os.Setenv(EnvDownloadBaseURL, cdn.URL)
	defer os.Setenv(EnvDownloadBaseURL, oldDownloadBase)

	githubArchiveURL := "https://downloads.example.test/" + archive
	githubChecksumURL := "https://downloads.example.test/" + checksum
	SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"tag_name":"v` + version + `","assets":[` +
			`{"name":"` + archive + `","browser_download_url":"` + githubArchiveURL + `"},` +
			`{"name":"` + checksum + `","browser_download_url":"` + githubChecksumURL + `"}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})})
	defer SetHTTPClient(&http.Client{Timeout: DefaultHTTPTimeout})

	source, err := ResolveAssetSource(version)
	if err != nil {
		t.Fatal(err)
	}
	if source.ArchiveURL != githubArchiveURL {
		t.Fatalf("archive URL = %q, want GitHub URL %q", source.ArchiveURL, githubArchiveURL)
	}
	if source.ChecksumURL != githubChecksumURL {
		t.Fatalf("checksum URL = %q, want GitHub URL %q", source.ChecksumURL, githubChecksumURL)
	}
}
