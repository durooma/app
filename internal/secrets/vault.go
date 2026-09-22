// Package secrets encrypts stored credentials with a server key kept outside the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Vault struct{ aead cipher.AEAD }

func New(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("credential encryption requires a 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

// LoadOrCreate publishes a complete random key atomically, without replacing an
// existing key. Back up this file separately from database backups.
func LoadOrCreate(path string) (*Vault, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create credential key directory: %w", err)
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(filepath.Dir(path), ".credential-key-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(key); err != nil {
			f.Close()
			return nil, err
		}
		if err = f.Sync(); err != nil {
			f.Close()
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
		if err = os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("credential key must be a regular file accessible only to its owner (0600)")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return New(key)
}

func (v *Vault) Seal(owner, key string) []byte {
	return append([]byte{1}, v.aead.Seal(nil, nil, []byte(key), []byte("openrouter:"+owner))...)
}
func (v *Vault) Open(owner string, encrypted []byte) (string, error) {
	if len(encrypted) < 1 || encrypted[0] != 1 {
		return "", errors.New("stored API key cannot be decrypted")
	}
	plain, err := v.aead.Open(nil, nil, encrypted[1:], []byte("openrouter:"+owner))
	if err != nil {
		return "", errors.New("stored API key cannot be decrypted; restore the server credential key")
	}
	return string(plain), nil
}
