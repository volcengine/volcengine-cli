package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestParseChecksumEntries(t *testing.T) {
	content := `
39930a55f0ee4492bca789d5c4198c790777f90d6080b9fdb49b89bae2430ac8  volcengine-cli_1.0.49_darwin_amd64.tar.gz
f241502edcfa7c1420c52f346b0405edd34243b30d0ef61db23ff5c9fed4e26e  volcengine-cli_1.0.49_darwin_amd64.zip
deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef *volcengine-cli_1.0.49_linux_amd64.zip
`
	entries, err := ParseChecksumEntries(content)
	if err != nil {
		t.Fatal(err)
	}
	if got := entries["volcengine-cli_1.0.49_darwin_amd64.zip"]; got != "f241502edcfa7c1420c52f346b0405edd34243b30d0ef61db23ff5c9fed4e26e" {
		t.Fatalf("zip hash: %s", got)
	}
	if got := entries["volcengine-cli_1.0.49_linux_amd64.zip"]; got != "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" {
		t.Fatalf("star prefix hash: %s", got)
	}
}

func TestChecksumForArchive(t *testing.T) {
	content := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  foo.zip\n"
	hash, err := ChecksumForArchive(content, "foo.zip")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatal(hash)
	}
	_, err = ChecksumForArchive(content, "bar.zip")
	if err == nil {
		t.Fatal("expected missing entry error")
	}
}

func TestVerifyFileChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	data := []byte("hello-upgrade")
	if err := ioutil.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	if err := VerifyFileChecksum(path, hexSum); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFileChecksum(path, "00"+hexSum[2:]); err == nil {
		t.Fatal("expected mismatch")
	}
	_ = os.Remove(path)
}
