package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "creds.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHandleArgumentsVersionHelpAndUnknown(t *testing.T) {
	var out bytes.Buffer
	if !handleArguments([]string{"--version"}, &out) {
		t.Fatal("--version should be handled")
	}
	if !strings.Contains(out.String(), "gochat "+Version) {
		t.Fatalf("version output = %q", out.String())
	}

	out.Reset()
	if !handleArguments([]string{"--help"}, &out) {
		t.Fatal("--help should be handled")
	}
	if !strings.Contains(out.String(), "usage: gochat") {
		t.Fatalf("help output = %q", out.String())
	}

	out.Reset()
	if handleArguments([]string{"--unknown", "extra", "-x"}, &out) {
		t.Fatal("unknown arguments should be ignored")
	}
	if out.Len() != 0 {
		t.Fatalf("unknown arguments should print nothing: %q", out.String())
	}
}

func TestRequireCredentials(t *testing.T) {
	missing, err := requireCredentials(map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "m"})
	if err != nil || len(missing) != 0 {
		t.Fatalf("complete environment -> %v, %v", missing, err)
	}

	empty := writeEnvFile(t, "")
	missing, err = requireCredentials(map[string]string{"AI_CHAT_ENV_FILE": empty})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "OLLAMA_API_KEY,OLLAMA_MODEL" {
		t.Fatalf("missing = %v", missing)
	}

	missing, err = requireCredentials(map[string]string{
		"OLLAMA_API_KEY":   "k",
		"OLLAMA_MODEL":     "   ",
		"AI_CHAT_ENV_FILE": writeEnvFile(t, "OLLAMA_MODEL=\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "OLLAMA_MODEL" {
		t.Fatalf("a whitespace model should be missing: %v", missing)
	}
}

func TestRequireCredentialsMissingExplicitFile(t *testing.T) {
	_, err := requireCredentials(map[string]string{
		"AI_CHAT_ENV_FILE": filepath.Join(t.TempDir(), "absent.env"),
	})
	if err == nil {
		t.Fatal("want an error for a missing explicit configuration file")
	}
}
