package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultHTTPTimeout is used for upgrade downloads (longer than version checks).
const DefaultHTTPTimeout = 120 * time.Second

// CheckHTTPTimeout is used for lightweight version detection.
const CheckHTTPTimeout = 1500 * time.Millisecond

var (
	httpClient = &http.Client{Timeout: DefaultHTTPTimeout}
	// checkHTTPClient is a short-timeout client for background version checks.
	checkHTTPClient = &http.Client{Timeout: CheckHTTPTimeout}
)

// SetHTTPClient overrides the download client (tests).
func SetHTTPClient(c *http.Client) {
	if c != nil {
		httpClient = c
	}
}

// SetCheckHTTPClient overrides the version-check client (tests).
func SetCheckHTTPClient(c *http.Client) {
	if c != nil {
		checkHTTPClient = c
	}
}

// DownloadFile downloads url into destPath. Progress is written to w when non-nil.
// Content is written to a same-directory temp file first, then renamed to destPath so a
// failed/partial download never leaves a truncated final path. When Content-Length is
// present, the transferred size must match.
func DownloadFile(w io.Writer, url, destPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d for %s", resp.StatusCode, url)
	}

	tmpDir := filepath.Dir(destPath)
	if tmpDir == "" {
		tmpDir = "."
	}
	tmpFile, err := ioutil.TempFile(tmpDir, ".ve-download-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	// Ensure temp is removed on any failure path; success path renames it away.
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	totalSize := resp.ContentLength
	var downloaded int64
	var lastPct int64 = -1
	buf := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				tmpFile.Close()
				return writeErr
			}
			downloaded += int64(n)
			if w != nil && totalSize > 0 {
				pct := downloaded * 100 / totalSize
				if pct != lastPct {
					lastPct = pct
					fmt.Fprintf(w, "\r  Progress: %d%% (%s / %s)", pct, formatSize(downloaded), formatSize(totalSize))
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			tmpFile.Close()
			return readErr
		}
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if totalSize > 0 && downloaded != totalSize {
		return fmt.Errorf("download incomplete: expected %d bytes, got %d bytes", totalSize, downloaded)
	}
	if w != nil {
		fmt.Fprintf(w, "\r  Download complete.                                  \n")
	}

	_ = os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	success = true
	return nil
}

// FetchURLBytes GETs url and returns the body (limited for small payloads).
func FetchURLBytes(client *http.Client, url string, limit int64) ([]byte, error) {
	if client == nil {
		client = httpClient
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", url, resp.StatusCode)
	}
	if limit <= 0 {
		limit = 1 << 20
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
