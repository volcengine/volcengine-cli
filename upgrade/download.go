package upgrade

// Copyright 2022 Beijing Volcanoengine Technology Ltd.  All Rights Reserved.

import (
	"fmt"
	"io"
	"net/http"
	"os"
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
func DownloadFile(w io.Writer, url, destPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d for %s", resp.StatusCode, url)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	var downloaded int64
	var lastPct int64 = -1
	buf := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
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
			return readErr
		}
	}
	if w != nil {
		fmt.Fprintf(w, "\r  Download complete.                                  \n")
	}
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
