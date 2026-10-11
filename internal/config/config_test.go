package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseEnvFileSubset(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "# comment\n\n  export OLLAMA_MODEL=quoted value \nOLLAMA_API_KEY='single'\nBAD KEY=no\nNOEQUALS\nDUP=one\nDUP=two\n")

	values := ParseEnvFile(path)

	if values["OLLAMA_MODEL"] != "quoted value" {
		t.Fatalf("OLLAMA_MODEL = %q", values["OLLAMA_MODEL"])
	}
	if values["OLLAMA_API_KEY"] != "single" {
		t.Fatalf("OLLAMA_API_KEY = %q", values["OLLAMA_API_KEY"])
	}
	if _, ok := values["BAD KEY"]; ok {
		t.Fatal("invalid key accepted")
	}
	if _, ok := values["NOEQUALS"]; ok {
		t.Fatal("line without = accepted")
	}
	if values["DUP"] != "two" {
		t.Fatalf("DUP = %q, want last assignment", values["DUP"])
	}
}

func TestParseEnvFileMissingIsEmpty(t *testing.T) {
	if got := ParseEnvFile(filepath.Join(t.TempDir(), "absent.env")); len(got) != 0 {
		t.Fatalf("want empty map, got %v", got)
	}
}

func TestLoadPrefersEnvironment(t *testing.T) {
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".env"), "OLLAMA_API_KEY=file\nOLLAMA_MODEL=file\n")

	env := map[string]string{"OLLAMA_API_KEY": "env", "OLLAMA_MODEL": "env"}
	if err := Load(env, cwd, cwd); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "env" || env["OLLAMA_MODEL"] != "env" {
		t.Fatalf("environment should win: %v", env)
	}
}

func TestLoadCwdThenHome(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".env"), "OLLAMA_API_KEY=cwd\n")
	writeFile(t, filepath.Join(home, ".env"), "OLLAMA_API_KEY=home\nOLLAMA_MODEL=home-model\n")

	env := map[string]string{}
	if err := Load(env, cwd, home); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "cwd" {
		t.Fatalf("cwd should win for key: %v", env)
	}
	if env["OLLAMA_MODEL"] != "home-model" {
		t.Fatalf("home should fill the blank model: %v", env)
	}
}

func TestLoadBlankValueFallsThrough(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".env"), "OLLAMA_API_KEY=\nOLLAMA_MODEL=cwd-model\n")
	writeFile(t, filepath.Join(home, ".env"), "OLLAMA_API_KEY=home-key\n")

	env := map[string]string{}
	if err := Load(env, cwd, home); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "home-key" {
		t.Fatalf("blank cwd value should fall through: %v", env)
	}
	if env["OLLAMA_MODEL"] != "cwd-model" {
		t.Fatalf("model = %q", env["OLLAMA_MODEL"])
	}
}

func TestLoadExplicitFileMissing(t *testing.T) {
	cwd := t.TempDir()
	env := map[string]string{"AI_CHAT_ENV_FILE": "missing.env"}
	err := Load(env, cwd, cwd)
	if err == nil {
		t.Fatal("want error for missing explicit file")
	}
	var configErr *ConfigurationError
	if !asConfigurationError(err, &configErr) {
		t.Fatalf("error type = %T", err)
	}
}

func TestLoadExplicitFileRelativeToCwd(t *testing.T) {
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, "private.env"), "OLLAMA_API_KEY=explicit\nOLLAMA_MODEL=explicit-model\n")

	env := map[string]string{"AI_CHAT_ENV_FILE": "private.env"}
	if err := Load(env, cwd, cwd); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "explicit" {
		t.Fatalf("explicit file not loaded: %v", env)
	}
}

func TestIsConfigured(t *testing.T) {
	if IsConfigured(nil) {
		t.Fatal("nil env should not be configured")
	}
	if IsConfigured(map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": " "}) {
		t.Fatal("blank model should not be configured")
	}
	if !IsConfigured(map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "m"}) {
		t.Fatal("complete env should be configured")
	}
}

func asConfigurationError(err error, target **ConfigurationError) bool {
	configErr, ok := err.(*ConfigurationError)
	if ok {
		*target = configErr
	}
	return ok
}

func TestLoadConfiguredEnvironmentSkipsFiles(t *testing.T) {
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".env"), "OLLAMA_API_KEY=file\nOLLAMA_MODEL=file\n")

	env := map[string]string{
		"OLLAMA_API_KEY":   "env",
		"OLLAMA_MODEL":     "env",
		"AI_CHAT_ENV_FILE": "missing.env",
	}
	if err := Load(env, cwd, cwd); err != nil {
		t.Fatalf("a complete environment should not read or require files: %v", err)
	}
	if env["OLLAMA_API_KEY"] != "env" || env["OLLAMA_MODEL"] != "env" {
		t.Fatalf("environment was modified: %v", env)
	}
}

func TestLoadBlankEnvironmentValueIsFilledFromCurrentDirectory(t *testing.T) {
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".env"), "OLLAMA_API_KEY=cwd-key\nOLLAMA_MODEL=cwd-model\n")

	env := map[string]string{"OLLAMA_MODEL": "   "}
	if err := Load(env, cwd, cwd); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "cwd-key" || env["OLLAMA_MODEL"] != "cwd-model" {
		t.Fatalf("blank environment value should be filled: %v", env)
	}
}

func TestLoadUsesHomeFromTheEnvironment(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".env"), "OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n")

	env := map[string]string{"HOME": home}
	if err := Load(env, cwd, ""); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "home-key" || env["OLLAMA_MODEL"] != "home-model" {
		t.Fatalf("HOME from the environment was not used: %v", env)
	}
}

func TestLoadKeepsNonBlankValuesWhileFillingBlanks(t *testing.T) {
	cwd := t.TempDir()
	writeFile(t, filepath.Join(cwd, ".env"), "OLLAMA_API_KEY=file-key\nOLLAMA_MODEL=file-model\n")

	env := map[string]string{"OLLAMA_API_KEY": "keep", "OLLAMA_MODEL": ""}
	if err := Load(env, cwd, cwd); err != nil {
		t.Fatal(err)
	}
	if env["OLLAMA_API_KEY"] != "keep" {
		t.Fatalf("non-blank value was overwritten: %v", env)
	}
	if env["OLLAMA_MODEL"] != "file-model" {
		t.Fatalf("blank value was not filled: %v", env)
	}
}
