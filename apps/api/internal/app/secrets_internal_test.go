package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestSealUnsealAndKeyFile(t *testing.T) {
	dir := t.TempDir()
	key, err := LoadSecretKey(dir, "")
	if err != nil || len(key) != 32 {
		t.Fatalf("new key: %v %d", err, len(key))
	}
	if st, err := os.Stat(filepath.Join(dir, "secret.key")); err != nil || st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key file: %v %v", err, st)
	}
	again, err := LoadSecretKey(dir, "")
	if err != nil || string(again) != string(key) {
		t.Fatal("the key file is not reused")
	}
	fromEnv, err := LoadSecretKey(t.TempDir(), base64.StdEncoding.EncodeToString(key))
	if err != nil || string(fromEnv) != string(key) {
		t.Fatalf("env key: %v", err)
	}
	if _, err := LoadSecretKey(t.TempDir(), "short"); err == nil {
		t.Fatal("a bad env key was accepted")
	}

	s := &Service{Cfg: Config{SecretKey: key}}
	sealed, err := s.seal(map[string]string{"token": "hunter2"})
	if err != nil || sealed == "" || containsPlain(sealed, "hunter2") {
		t.Fatalf("seal: %q %v", sealed, err)
	}
	var out map[string]string
	if err := s.unseal(sealed, &out); err != nil || out["token"] != "hunter2" {
		t.Fatalf("unseal: %v %v", out, err)
	}
	// A key differing in one byte (flipping it: appending 'x' was the same key whenever the byte was 'x').
	ok := append([]byte{}, key...)
	ok[31] ^= 0xff
	other := &Service{Cfg: Config{SecretKey: ok}}
	if err := other.unseal(sealed, &out); err == nil {
		t.Fatal("another key opened the box")
	}
	if _, err := (&Service{}).seal("x"); err == nil {
		t.Fatal("sealing without a key")
	}
}

func containsPlain(sealed, plain string) bool {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	return err == nil && stringContains(string(raw), plain)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
