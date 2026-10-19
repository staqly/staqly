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
	if b, err := os.ReadFile(path); err == nil {
		k, err := base64.StdEncoding.DecodeString(string(b))
		if err != nil || len(k) != 32 {
			return nil, fmt.Errorf("%s is not a valid key; remove it to generate a new one (channels will need reconnecting)", path)
		}
		return k, nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(k)), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

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
