package upgrade

import (
	"archive/zip"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveAndChecksumNames(t *testing.T) {
	if got := ArchiveName("v1.0.49", "windows", "amd64"); got != "volcengine-cli_1.0.49_windows_amd64.zip" {
		t.Fatal(got)
	}
	if got := ChecksumName("1.0.49"); got != "volcengine-cli_1.0.49_SHA256SUMS" {
		t.Fatal(got)
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	if err := writeTestZip(zipPath, "ve.exe", []byte("binary-content")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.exe")
	if err := ExtractBinaryFromZip(zipPath, dest, "ve.exe"); err != nil {
		t.Fatal(err)
	}
	got, err := ioutil.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "binary-content" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractBinaryFromZip_NotFound(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")
	if err := writeTestZip(zipPath, "other", []byte("x")); err != nil {
		t.Fatal(err)
	}
	err := ExtractBinaryFromZip(zipPath, filepath.Join(dir, "out"), "ve")
	if err == nil {
		t.Fatal("expected error")
	}
}

func writeTestZip(zipPath, name string, content []byte) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	fw, err := w.Create(name)
	if err != nil {
		return err
	}
	if _, err := fw.Write(content); err != nil {
		return err
	}
	return w.Close()
}
