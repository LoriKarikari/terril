package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type envFlags struct {
	names  []string
	values map[string]string
}

func (e *envFlags) String() string { return strings.Join(e.names, ",") }

func (e *envFlags) Set(s string) error {
	name, value, hasValue := strings.Cut(s, "=")
	if !envNameRe.MatchString(name) {
		return fmt.Errorf("%q isn't a valid variable name", name)
	}
	if !hasValue {
		value, hasValue = os.LookupEnv(name)
	}
	if !slices.Contains(e.names, name) {
		e.names = append(e.names, name)
	}
	if e.values == nil {
		e.values = map[string]string{}
	}
	if hasValue {
		e.values[name] = value
	} else {
		delete(e.values, name)
	}
	return nil
}

func (e *envFlags) check() error {
	for _, n := range e.names {
		if _, ok := e.values[n]; !ok {
			return fmt.Errorf("--env %s: %s isn't set in the environment; pass --env %s=VALUE", n, n, n)
		}
	}
	return nil
}

// The digest sits on docker's command line and in the image history, so it's keyed with a
// secret from this machine. Without the key, a short secret can't be confirmed by hashing guesses.
func (e *envFlags) digest() (string, error) {
	key, err := digestKey()
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, key)
	for _, n := range slices.Sorted(slices.Values(e.names)) {
		h.Write([]byte(n + "=" + e.values[n] + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func digestKey() ([]byte, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("--env needs a cache directory for its key: %w", err)
	}
	p := filepath.Join(dir, "terril", "build-env.key")
	if key, err := os.ReadFile(p); err == nil && len(key) == 32 {
		return key, nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	return key, os.WriteFile(p, key, 0o600)
}
