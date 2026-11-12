package app

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// Processes that start together on a fresh data directory (the server and mcp-stdio, two containers on one
// volume) must all end up with the key that is on disk; the same goes for the access token.
func TestConcurrentFirstStartsShareOneSecret(t *testing.T) {
	for round := range 100 {
		dir := t.TempDir()
		const n = 8
		keys, tokens := make([][]byte, n), make([]string, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range n {
			wg.Go(func() {
				<-start
				var err error
				if keys[i], err = LoadSecretKey(dir, ""); err != nil {
					t.Error(err)
				}
				if tokens[i], err = (&Service{Cfg: Config{LocalTokenPath: filepath.Join(dir, LocalTokenFile)}}).EnsureLocalToken(); err != nil {
					t.Error(err)
				}
			})
		}
		close(start)
		wg.Wait()
		raw, _ := os.ReadFile(filepath.Join(dir, "secret.key"))
		disk, _ := base64.StdEncoding.DecodeString(string(raw))
		tok, _ := os.ReadFile(filepath.Join(dir, LocalTokenFile))
		for i := range n {
			if !bytes.Equal(keys[i], disk) {
				t.Fatalf("round %d: starter %d holds a key that is not the one in secret.key (what it seals cannot be opened after a restart)", round, i)
			}
			if tokens[i] != strings.TrimSpace(string(tok)) {
				t.Fatalf("round %d: starter %d holds an access token that is not the one in the file", round, i)
			}
		}
	}
}

// A key file that is there but cannot be read is an error, never a reason to make a new key over it.
func TestUnreadableSecretKeyIsNotReplaced(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every file")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.key")
	if _, err := LoadSecretKey(dir, ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0o200); err != nil { // writable, not readable
		t.Fatal(err)
	}
	_, err := LoadSecretKey(dir, "")
	_ = os.Chmod(path, 0o600)
	if after, _ := os.ReadFile(path); err == nil || !bytes.Equal(before, after) {
		t.Fatalf("a key file that could not be read was replaced by a new key (err %v)", err)
	}
}
