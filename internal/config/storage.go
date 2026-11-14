package config

import (
	"annoying-web-blocker-desktop/internal/platformpaths"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var (
	DefaultPath = filepath.Join(platformpaths.ApplicationDir, "config.bin")
	LegacyPath  = filepath.Join(platformpaths.ApplicationDir, "config.json")
)

const (
	fileMagic = "AWBCFG\x01"
	keySize   = 32
)

// Initialize creates encrypted storage or migrates config.json beside it.
func Initialize(path string) error {
	if _, err := os.Stat(path); err == nil {
		_, err = Load(path)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if StoragePresent(path) {
		c, err := Load(path)
		if err != nil {
			return err
		}
		return Save(path, c)
	}

	legacyPath := filepath.Join(filepath.Dir(path), "config.json")
	if data, err := os.ReadFile(legacyPath); err == nil {
		c, err := decodeJSON(data)
		if err != nil {
			return fmt.Errorf("migrate legacy configuration: %w", err)
		}
		if err := Save(path, c); err != nil {
			return fmt.Errorf("encrypt legacy configuration: %w", err)
		}
		if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove legacy configuration: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return Save(path, Default())
}

func StoragePresent(path string) bool {
	for _, candidate := range []string{path, path + ".bak", path + ".key"} {
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

// Load authenticates and decrypts the primary file, falling back to its last
// known-good encrypted backup. Missing storage is an error rather than an empty
// schedule so installed services do not fail open.
func Load(path string) (Config, error) {
	c, primaryErr := loadFile(path)
	if primaryErr == nil {
		return c, nil
	}
	c, backupErr := loadFile(path + ".bak")
	if backupErr == nil {
		return c, nil
	}
	return Config{}, fmt.Errorf("configuration unavailable (primary: %v; backup: %v)", primaryErr, backupErr)
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	key, err := readKey(pathForKey(path))
	if err != nil {
		return Config{}, err
	}
	plain, err := decrypt(data, key)
	if err != nil {
		return Config{}, err
	}
	return decodeJSON(plain)
}

func Save(path string, c Config) error {
	validated, err := c.Validate()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(validated)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	keyPath := pathForKey(path)
	key, err := loadOrCreateKey(keyPath, path)
	if err != nil {
		return err
	}
	encrypted, err := encrypt(plain, key)
	if err != nil {
		return err
	}

	if old, err := os.ReadFile(path); err == nil {
		// Back up only a file which still authenticates with the current key.
		if _, decryptErr := decrypt(old, key); decryptErr == nil {
			if err := atomicWrite(path+".bak", old, 0600); err != nil {
				return fmt.Errorf("write configuration backup: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(path, encrypted, 0600)
}

func decodeJSON(data []byte) (Config, error) {
	var raw struct {
		Sites   []Site   `json:"sites"`
		Enabled *bool    `json:"enabled"`
		Start   string   `json:"start"`
		End     string   `json:"end"`
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	c := Config{Sites: raw.Sites}
	if raw.Sites == nil && raw.Enabled != nil {
		for _, domain := range raw.Domains {
			c.Sites = append(c.Sites, Site{
				Domain: domain, Enabled: *raw.Enabled,
				Schedules: []Schedule{{Start: raw.Start, End: raw.End}},
			})
		}
	}
	return c.Validate()
}

func encrypt(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	result := append([]byte(fileMagic), nonce...)
	return aead.Seal(result, nonce, plain, []byte(fileMagic)), nil
}

func decrypt(data, key []byte) ([]byte, error) {
	if len(data) < len(fileMagic) || string(data[:len(fileMagic)]) != fileMagic {
		return nil, errors.New("invalid encrypted configuration header")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < len(fileMagic)+aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("encrypted configuration is truncated")
	}
	nonce := data[len(fileMagic) : len(fileMagic)+aead.NonceSize()]
	ciphertext := data[len(fileMagic)+aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, []byte(fileMagic))
	if err != nil {
		return nil, errors.New("encrypted configuration authentication failed")
	}
	return plain, nil
}

func pathForKey(path string) string {
	if filepath.Ext(path) == ".bak" {
		path = path[:len(path)-len(".bak")]
	}
	return path + ".key"
}

func loadOrCreateKey(keyPath, configPath string) ([]byte, error) {
	key, err := readKey(keyPath)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if _, configErr := os.Stat(configPath); configErr == nil {
		return nil, errors.New("configuration key is missing")
	} else if !errors.Is(configErr, os.ErrNotExist) {
		return nil, configErr
	}
	key = make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	protected, err := protectKey(key)
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(keyPath, protected, 0600); err != nil {
		return nil, err
	}
	if err := secureKeyFile(keyPath); err != nil {
		_ = os.Remove(keyPath)
		return nil, err
	}
	return key, nil
}

func readKey(path string) ([]byte, error) {
	protected, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := unprotectKey(protected)
	if err != nil {
		return nil, fmt.Errorf("unprotect configuration key: %w", err)
	}
	if len(key) != keySize {
		return nil, errors.New("configuration key has invalid length")
	}
	return key, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-storage-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
