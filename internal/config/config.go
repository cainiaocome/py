// Package config resolves Ollama Cloud credentials from the environment and
// .env files, mirroring the Python launcher's lookup order.
//
// Values already present and non-blank in the environment win, then the current
// directory's .env, then the home directory's .env. AI_CHAT_ENV_FILE selects one
// explicit file instead of those defaults. Only a safe subset of shell syntax is
// supported: blank lines, # comments, an optional leading export, KEY=value
// pairs, and single- or double-quoted values. The parser never executes the file
// and never expands variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CredentialNames are the variables the application needs to run.
var CredentialNames = []string{"OLLAMA_API_KEY", "OLLAMA_MODEL"}

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ConfigurationError reports an explicitly requested configuration file that
// does not exist.
type ConfigurationError struct {
	Path string
}

func (e *ConfigurationError) Error() string {
	return fmt.Sprintf("Configuration file not found: %s", e.Path)
}

// Environ snapshots the process environment into a map.
func Environ() map[string]string {
	env := make(map[string]string, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		env[key] = value
	}
	return env
}

// IsConfigured reports whether both credentials hold a non-blank value.
func IsConfigured(env map[string]string) bool {
	for _, name := range CredentialNames {
		if strings.TrimSpace(env[name]) == "" {
			return false
		}
	}
	return true
}

// ParseEnvFile parses the supported .env subset. Later assignments win. A
// missing or unreadable file yields an empty map.
func ParseEnvFile(path string) map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimSpace(line[len("export"):])
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if !keyPattern.MatchString(key) {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			quote := value[0]
			if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
				value = value[1 : len(value)-1]
			}
		}
		values[key] = value
	}
	return values
}

// Load fills the credential variables in env from the environment and files.
// Values already present and non-blank are never overwritten. A blank value in
// the current directory's file does not stop a later file from filling it. It
// returns a *ConfigurationError when AI_CHAT_ENV_FILE names a missing file.
//
// cwd and home override the defaults when non-empty; they exist for tests.
func Load(env map[string]string, cwd, home string) error {
	if IsConfigured(env) {
		return nil
	}
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	explicit := strings.TrimSpace(env["AI_CHAT_ENV_FILE"])
	if explicit != "" {
		path := explicit
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return &ConfigurationError{Path: path}
		}
		apply(env, path)
		return nil
	}

	cwdPath := filepath.Join(cwd, ".env")
	apply(env, cwdPath)
	if IsConfigured(env) {
		return nil
	}
	homeDir := homeDirectory(env, home)
	if homeDir == "" {
		return nil
	}
	homePath := filepath.Join(homeDir, ".env")
	if homePath != cwdPath {
		apply(env, homePath)
	}
	return nil
}

func apply(env map[string]string, path string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	for key, value := range ParseEnvFile(path) {
		if strings.TrimSpace(env[key]) == "" {
			env[key] = value
		}
	}
}

func homeDirectory(env map[string]string, home string) string {
	if home != "" {
		return home
	}
	if configured := strings.TrimSpace(env["HOME"]); configured != "" {
		return configured
	}
	if dir, err := os.UserHomeDir(); err == nil {
		return dir
	}
	return ""
}
