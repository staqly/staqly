package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Channel credentials and developer app secrets are sealed with AES-256-GCM
// under one key per install: DATA_DIR/secret.key (created on first start,
// 0600) or STAQLY_SECRET_KEY (base64, 32 bytes; cloud deployments).

// LoadSecretKey returns the sealing key, creating the key file when neither it nor the env value exists.
func LoadSecretKey(dataDir, envValue string) ([]byte, error) {
	if envValue != "" {
		k, err := base64.StdEncoding.DecodeString(envValue)
		if err != nil || len(k) != 32 {
			return nil, errors.New("STAQLY_SECRET_KEY must be 32 bytes, base64-encoded (e.g. `openssl rand -base64 32`)")
		}
		return k, nil
	}
	path := filepath.Join(dataDir, "secret.key")
	// Only a missing file leads to a new key (never an unreadable one: that would replace the key the
	// stored credentials are sealed with). Processes starting together on a fresh data directory (the
	// server and mcp-stdio) race for the exclusive create; the losers read the winner's key.
	b, err := readFullKey(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dataDir, 0o750); err != nil {
			return nil, err
		}
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, err
		}
		f, ferr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if ferr == nil {
			_, err = f.WriteString(base64.StdEncoding.EncodeToString(k))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			return k, err
		}
		if !errors.Is(ferr, os.ErrExist) {
			return nil, ferr
		}
		b, err = readFullKey(path)
	}
	if err != nil {
		return nil, err
	}
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(k) != 32 {
		return nil, fmt.Errorf("%s is not a valid key; remove it to generate a new one (channels will need reconnecting)", path)
	}
	return k, nil
}

// errUnreadableCredentials is what a target, a channel and its alerts say when the sealed credentials don't open.
const errUnreadableCredentials = "stored credentials cannot be read with this server's secret key (restored without secret.key, or the key changed): reconnect the channel"

var errNoSecretKey = errors.New("no secret key configured (DATA_DIR/secret.key or STAQLY_SECRET_KEY)")

// seal encrypts v as JSON; the result is base64 (nonce + ciphertext).
func (s *Service) seal(v any) (string, error) {
	if len(s.Cfg.SecretKey) != 32 {
		return "", errNoSecretKey
	}
	block, err := aes.NewCipher(s.Cfg.SecretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	plain, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

// unseal decrypts what seal produced into v. An empty value leaves v as it is.
func (s *Service) unseal(sealed string, v any) error {
	if sealed == "" {
		return nil
	}
	if len(s.Cfg.SecretKey) != 32 {
		return errNoSecretKey
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(s.Cfg.SecretKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(raw) < gcm.NonceSize() {
		return errors.New("sealed value too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return errors.New("sealed value cannot be opened with this server's key")
	}
	return json.Unmarshal(plain, v)
}
