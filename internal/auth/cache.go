package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"

	"github.com/sd-strax/daakiya/internal/secrets"
)

// AccountCacheKey is the Keychain account holding the AES key for the cache file.
const AccountCacheKey = "msal-cache-key"

// CacheFileName is the encrypted MSAL token cache under the data directory.
const CacheFileName = "msal-cache.bin"

// fileCache keeps MSAL's opaque cache blob (several KB, too large for the
// `security` helper) in a file encrypted with AES-GCM. The 32-byte key lives in
// the Keychain, so the file alone is useless.
type fileCache struct {
	secrets secrets.Store
	path    string
}

func (f fileCache) key(create bool) ([]byte, error) {
	b64, err := f.secrets.Get(AccountCacheKey)
	if errors.Is(err, secrets.ErrNotFound) {
		if !create {
			return nil, err
		}
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		if err := f.secrets.Set(AccountCacheKey, base64.StdEncoding.EncodeToString(k)); err != nil {
			return nil, err
		}
		return k, nil
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(b64)
}

func (f fileCache) Replace(ctx context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
	data, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	k, err := f.key(false)
	if errors.Is(err, secrets.ErrNotFound) {
		return nil // key gone: cache is unreadable, start fresh
	}
	if err != nil {
		return err
	}
	plain, err := decrypt(k, data)
	if err != nil {
		return nil // corrupt or foreign file: start fresh rather than fail
	}
	return c.Unmarshal(plain)
}

func (f fileCache) Export(ctx context.Context, c cache.Marshaler, _ cache.ExportHints) error {
	plain, err := c.Marshal()
	if err != nil {
		return err
	}
	k, err := f.key(true)
	if err != nil {
		return err
	}
	data, err := encrypt(k, plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// Clear removes the cache file and its key.
func (f fileCache) Clear() error {
	if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return f.secrets.Delete(AccountCacheKey)
}

func encrypt(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plain, nil)...), nil
}

func decrypt(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("cache file too short")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}
