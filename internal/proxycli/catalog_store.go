package proxycli

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const DefaultCatalogURL = "https://cloudcache.volccdn.com/ve/proxycli/catalog-v2.json"
const CatalogFileName = "catalog-v2.json"

// Empty until the release owner provisions production keys. No env/CLI option
// can disable signature verification or supply a remote URL or trusted key.
var catalogSigningKeys = map[string]ed25519.PublicKey{}

type signedCatalog struct {
	KeyID     string `json:"key_id"`
	Payload   string `json:"payload"`   // base64 exact catalog JSON bytes
	Signature string `json:"signature"` // Ed25519 over decoded payload
}

type CatalogStore struct{ Dir string }

func NewCatalogStore() (*CatalogStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &CatalogStore{Dir: filepath.Join(home, ".volcengine", "proxy-cli")}, nil
}

// Load is local-only and does not create directories or read business credentials.
// A malformed cache falls back to the previous valid cache, then the embedded data.
func (s *CatalogStore) Load() *Snapshot {
	base := EmbeddedSnapshot()
	for _, name := range []string{CatalogFileName, CatalogFileName + ".previous"} {
		data, err := readCatalogFile(filepath.Join(s.Dir, name))
		if err != nil {
			continue
		}
		update, err := ParseCatalog(data)
		if err != nil {
			continue
		}
		update.source = "cache"
		if merged, err := overlayCatalog(base, update, false); err == nil {
			return merged
		}
	}
	return base
}

// ReadCatalogFile validates a bounded local publication without changing cache.
func ReadCatalogFile(path string) (*Snapshot, error) {
	data, err := readCatalogFile(path)
	if err != nil {
		return nil, err
	}
	return ParseCatalog(data)
}

func readCatalogFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err == nil && int64(len(data)) > maxManifestBytes {
		err = fmt.Errorf("catalog exceeds size limit")
	}
	return data, err
}

// RefreshFile explicitly imports a reviewed local catalog. It uses exactly the
// production schema/URL checks, with no arbitrary download hosts or scripts.
func (s *CatalogStore) RefreshFile(path string) (*Snapshot, error) {
	data, err := readCatalogFile(path)
	if err != nil {
		return nil, err
	}
	return s.replace(data)
}
func (s *CatalogStore) Refresh() (*Snapshot, error) {
	if len(catalogSigningKeys) == 0 {
		return nil, fmt.Errorf("remote catalog publication is not configured: production signing keys are pending; use 've proxycli refresh --file <reviewed-catalog.json>' for a local catalog")
	}
	m := &Manager{}
	data, err := m.download(DefaultCatalogURL, 2*maxManifestBytes)
	if err != nil {
		return nil, err
	}
	payload, err := verifyCatalogSignature(data, catalogSigningKeys)
	if err != nil {
		return nil, err
	}
	return s.replace(payload)
}
func verifyCatalogSignature(data []byte, keys map[string]ed25519.PublicKey) ([]byte, error) {
	var envelope signedCatalog
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	key, ok := keys[envelope.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("unknown catalog signing key %q", envelope.KeyID)
	}
	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return nil, err
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(key, payload, signature) {
		return nil, fmt.Errorf("invalid catalog signature")
	}
	return payload, nil
}
func (s *CatalogStore) replace(data []byte) (*Snapshot, error) {
	update, err := ParseCatalog(data)
	if err != nil {
		return nil, err
	}
	lock, err := acquireFileLock(filepath.Join(s.Dir, CatalogFileName+".lock"), 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer lock.release()
	merged, err := overlayCatalog(s.Load(), update, false)
	if err != nil {
		return nil, err
	}
	canonical, err := json.MarshalIndent(merged.catalog, "", "  ")
	if err != nil {
		return nil, err
	}
	// Retain a complete, validated prior snapshot even if the current cache is corrupt.
	previous, err := json.MarshalIndent(s.Load().catalog, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(s.Dir, CatalogFileName+".previous"), previous, 0600); err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(s.Dir, CatalogFileName), canonical, 0600); err != nil {
		return nil, err
	}
	merged.source = "cache"
	return merged, nil
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".proxycli-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return replaceFile(f.Name(), path)
}
