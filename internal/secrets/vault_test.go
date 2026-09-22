package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialEncryptionAndOwnerBinding(t *testing.T) {
	v, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, b := v.Seal("alice", "sk-or-test-key"), v.Seal("alice", "sk-or-test-key")
	if bytes.Equal(a, b) || bytes.Contains(a, []byte("sk-or-test-key")) {
		t.Fatal("encryption must use fresh nonces and hide plaintext")
	}
	if got, err := v.Open("alice", a); err != nil || got != "sk-or-test-key" {
		t.Fatal("round trip failed")
	}
	if _, err := v.Open("bob", a); err == nil {
		t.Fatal("ciphertext transferable between owners")
	}
	a[len(a)-1] ^= 1
	if _, err := v.Open("alice", a); err == nil {
		t.Fatal("tampering accepted")
	}
	for _, bad := range [][]byte{nil, {1}, {2, 1, 2}} {
		if _, err := v.Open("alice", bad); err == nil {
			t.Fatal("corrupt ciphertext accepted")
		}
	}
	other, _ := New(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open("alice", b); err == nil {
		t.Fatal("wrong server key accepted")
	}
}
func TestServerKeyPersistsAndRejectsUnsafeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "credentials.key")
	v, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := v.Seal("owner", "credential")
	again, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Open("owner", encrypted); err != nil || got != "credential" {
		t.Fatal("server restart changed key")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("unsafe key permissions")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(link); err == nil {
		t.Fatal("key symlink accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("public key file accepted")
	}
}
