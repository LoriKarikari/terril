package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envFlags collects --env KEY=VALUE, or --env KEY to take the value from terril's own environment.
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

func (e *envFlags) digest() string {
	names := slices.Sorted(slices.Values(e.names))
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n + "=" + e.values[n] + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
