// Package auth implements NorthScope's built-in single-user login: a
// server-rendered sign-in form, HMAC-signed session cookies, login
// throttling, and middleware that protects the UI, API, and WebSocket.
package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	UsernameFile   = "username"
	PasswordFile   = "password"
	SessionKeyFile = "session-key"

	// MinSessionKeyLength rejects session keys too short to sign cookies safely.
	MinSessionKeyLength = 32
)

// Credentials are the single NorthScope login and the key that signs its
// session cookies.
type Credentials struct {
	Username   string
	Password   string
	SessionKey string
}

// LoadCredentials reads credentials from a directory that contains the files
// username, password, and session-key, such as a mounted Kubernetes Secret.
// One trailing newline is ignored so files written with echo or a text editor
// work. Every file must exist and be non-empty.
func LoadCredentials(dir string) (Credentials, error) {
	if strings.TrimSpace(dir) == "" {
		return Credentials{}, errors.New("auth directory is empty")
	}

	var creds Credentials
	for _, file := range []struct {
		name  string
		value *string
	}{
		{UsernameFile, &creds.Username},
		{PasswordFile, &creds.Password},
		{SessionKeyFile, &creds.SessionKey},
	} {
		path := filepath.Join(dir, file.name)
		data, err := os.ReadFile(path)
		if err != nil {
			return Credentials{}, fmt.Errorf("read auth file %s: %w", path, err)
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if value == "" {
			return Credentials{}, fmt.Errorf("auth file %s is empty", path)
		}
		*file.value = value
	}

	if err := creds.Validate(); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

// Validate reports whether the credentials can be used to sign in.
func (c Credentials) Validate() error {
	switch {
	case c.Username == "":
		return errors.New("auth username is empty")
	case c.Password == "":
		return errors.New("auth password is empty")
	case len(c.SessionKey) < MinSessionKeyLength:
		return fmt.Errorf("auth session key must be at least %d characters", MinSessionKeyLength)
	}
	return nil
}

// CookieSecureMode controls the Secure attribute of the session cookie.
type CookieSecureMode int

const (
	// CookieSecureAuto marks the cookie Secure when the request arrived over
	// TLS or through a proxy that reports X-Forwarded-Proto: https.
	CookieSecureAuto CookieSecureMode = iota
	CookieSecureAlways
	CookieSecureNever
)

// ParseCookieSecureMode parses the -auth-cookie-secure flag value.
func ParseCookieSecureMode(value string) (CookieSecureMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return CookieSecureAuto, nil
	case "true":
		return CookieSecureAlways, nil
	case "false":
		return CookieSecureNever, nil
	}
	return CookieSecureAuto, fmt.Errorf("invalid cookie secure mode %q: use auto, true, or false", value)
}

func (m CookieSecureMode) String() string {
	switch m {
	case CookieSecureAlways:
		return "true"
	case CookieSecureNever:
		return "false"
	}
	return "auto"
}
