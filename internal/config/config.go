// Package config resolves Ollama Cloud credentials from the environment and
// .env files, mirroring the Python launcher's lookup order.
//
// Values already present and non-blank in the environment win, then the current
// directory's .env, then the home directory's .env. AI_CHAT_ENV_FILE selects one
// explicit file instead of those defaults.
//
// The container launcher sources the file with a POSIX shell, so this parser
// reads the same subset the same way: blank lines, comments, an optional leading
// export, KEY=value pairs, single- or double-quoted values, and a comment after a
// value. It never executes the file and never expands variables, which is the one
// place the two launchers can still disagree.
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
		values[key] = parseValue(value)
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

// parseValue reads the right-hand side of an assignment the way a POSIX shell
// reads a value: quotes are removed and concatenated with the surrounding text,
// and a # that starts a word begins a comment. The container launcher sources the
// file, so this has to agree with it -- `KEY=secret # note` is a credential
// followed by a comment to a shell and `secret # note` to a naive parser, which
// is a 401 rather than a typo.
//
// Where a shell would reject the line -- unquoted spaces, for instance -- this
// parser is deliberately more forgiving.
func parseValue(raw string) string {
	value := strings.TrimSpace(raw)
	var text strings.Builder
	significant := 0 // unquoted trailing blanks after this point are dropped

	appendSegment := func(segment string, quoted bool) {
		text.WriteString(segment)
		if quoted || strings.Trim(segment, " \t") != "" {
			significant = text.Len()
		}
	}

	for index := 0; index < len(value); {
		switch character := value[index]; {
		case character == '#' && (index == 0 || value[index-1] == ' ' || value[index-1] == '\t'):
			return text.String()[:significant]
		case character == '"' || character == '\'':
			closing := closingQuote(value, index)
			if closing < 0 {
				// An unterminated quote is not ours to interpret; keep it as written.
				appendSegment(strings.TrimRight(value[index:], " \t"), false)
				return text.String()[:significant]
			}
			appendSegment(unquote(value[index:closing+1]), true)
			index = closing + 1
		default:
			appendSegment(string(character), false)
			index++
		}
	}
	return text.String()[:significant]
}

// closingQuote returns the index of the quote that closes the one at start, or -1.
// Only a double quote takes backslash escapes.
func closingQuote(value string, start int) int {
	quote := value[start]
	for index := start + 1; index < len(value); index++ {
		if quote == '"' && value[index] == '\\' {
			index++
			continue
		}
		if value[index] == quote {
			return index
		}
	}
	return -1
}

// unquote strips the surrounding quotes from a segment and resolves the escapes
// a double-quoted string allows.
func unquote(segment string) string {
	inner := segment[1 : len(segment)-1]
	if segment[0] == '\'' {
		return inner
	}
	var text strings.Builder
	for index := 0; index < len(inner); index++ {
		if character := inner[index]; character == '\\' && index+1 < len(inner) {
			if next := inner[index+1]; next == '"' || next == '\\' || next == '$' || next == '`' {
				index++
				text.WriteByte(next)
				continue
			}
		}
		text.WriteByte(inner[index])
	}
	return text.String()
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
