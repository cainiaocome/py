package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cainiaocome/py/internal/chat"
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

// fakeLister stands in for the chat backend so --check can be tested without a
// network.
type fakeLister struct {
	models  []string
	err     error
	pingErr error
	pinged  bool
}

func (f *fakeLister) ListModels(context.Context) ([]string, error) { return f.models, f.err }
func (f *fakeLister) Ping(context.Context) error                   { f.pinged = true; return f.pingErr }

func TestCheckReportsAMissingConfiguration(t *testing.T) {
	var out bytes.Buffer
	code := report(map[string]string{"OLLAMA_MODEL": "m"}, &out, &fakeLister{})
	if code == 0 {
		t.Fatal("an unconfigured client should fail the check")
	}
	if !strings.Contains(out.String(), "api key   unset") || !strings.Contains(out.String(), "OLLAMA_API_KEY") {
		t.Fatalf("output = %q", out.String())
	}
}

// The key is a secret: the check may say whether it is set, never what it is.
func TestCheckNeverPrintsTheAPIKey(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"OLLAMA_API_KEY": "super-secret-key", "OLLAMA_MODEL": "m"}
	if code := report(env, &out, &fakeLister{models: []string{"m", "other"}}); code != 0 {
		t.Fatalf("code = %d, output = %q", code, out.String())
	}
	if strings.Contains(out.String(), "super-secret-key") {
		t.Fatalf("the API key leaked into the report: %q", out.String())
	}
	if !strings.Contains(out.String(), "api key   set (16 characters)") {
		t.Fatalf("output = %q", out.String())
	}
	if !strings.Contains(out.String(), "result    OK") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCheckFailsWhenTheModelIsNotOffered(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "ghost"}
	code := report(env, &out, &fakeLister{models: []string{"real", "other"}})
	if code == 0 {
		t.Fatal("an unknown model should fail the check")
	}
	if !strings.Contains(out.String(), "ghost is not a model") || !strings.Contains(out.String(), "real") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCheckFailsWhenTheEndpointIsUnreachable(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "m"}
	code := report(env, &out, &fakeLister{err: errors.New("dial tcp: connection refused")})
	if code == 0 {
		t.Fatal("an unreachable endpoint should fail the check")
	}
	if !strings.Contains(out.String(), "dial tcp: connection refused") {
		t.Fatalf("output = %q", out.String())
	}
}

// The report runs against the real client, pointed at a local server, so the
// wiring is exercised end to end.
func TestCheckUsesTheRealClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer server.Close()

	env := map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "m"}
	backend := chat.NewWithEndpoints("k", "m", chat.Endpoints{ChatBase: server.URL})
	var out bytes.Buffer
	if code := report(env, &out, backend); code != 0 {
		t.Fatalf("code = %d, output = %q", code, out.String())
	}
	if !strings.Contains(out.String(), "models    1 available") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCheckSurfacesAnHTTPRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer server.Close()

	env := map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "m"}
	backend := chat.NewWithEndpoints("k", "m", chat.Endpoints{ChatBase: server.URL})
	var out bytes.Buffer
	if code := report(env, &out, backend); code == 0 {
		t.Fatal("a rejected key should fail the check")
	}
	if !strings.Contains(out.String(), "invalid api key") {
		t.Fatalf("output = %q", out.String())
	}
}

// Listing models does not authenticate, so the check must ask for a real
// completion before it calls the key good.
func TestCheckProbesTheChatEndpoint(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"OLLAMA_API_KEY": "k", "OLLAMA_MODEL": "m"}
	backend := &fakeLister{models: []string{"m"}}
	if code := report(env, &out, backend); code != 0 {
		t.Fatalf("code = %d, output = %q", code, out.String())
	}
	if !backend.pinged {
		t.Fatal("the check called the key good without a chat completion")
	}
	if !strings.Contains(out.String(), "chat completion") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestCheckFailsWhenTheChatEndpointRejectsTheKey(t *testing.T) {
	var out bytes.Buffer
	env := map[string]string{"OLLAMA_API_KEY": "bad", "OLLAMA_MODEL": "m"}
	backend := &fakeLister{models: []string{"m"}, pingErr: errors.New("chat completion failed (HTTP 401): Unauthorized")}
	if code := report(env, &out, backend); code == 0 {
		t.Fatal("a rejected key should fail the check")
	}
	if !strings.Contains(out.String(), "HTTP 401") || !strings.Contains(out.String(), "Unauthorized") {
		t.Fatalf("output = %q", out.String())
	}
	if strings.Contains(out.String(), "result    OK") {
		t.Fatalf("output = %q", out.String())
	}
}

// A value carrying a comment, a quote or a shell variable is not a credential,
// and the 401 it causes says nothing about why. The check has to name it, without
// ever printing the value.
func TestCheckWarnsAboutACredentialThatLooksMispared(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"trailing comment", "secret # my key", "contains whitespace"},
		{"unbalanced quote", `"secret`, "contains a quote"},
		{"shell reference", "${MY_KEY}", "shell variable reference"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			env := map[string]string{"OLLAMA_API_KEY": testCase.value, "OLLAMA_MODEL": "m"}
			var out bytes.Buffer
			report(env, &out, &fakeLister{models: []string{"m"}})
			if !strings.Contains(out.String(), testCase.want) {
				t.Fatalf("output = %q, want %q", out.String(), testCase.want)
			}
			if strings.Contains(out.String(), "secret") {
				t.Fatalf("the value leaked into the report: %q", out.String())
			}
		})
	}
}

func TestCheckDoesNotWarnAboutAWellFormedCredential(t *testing.T) {
	// The shape Ollama Cloud issues: 32 hex digits, a dot, then 24 characters.
	key := "0123456789abcdef0123456789abcdef.Abcdefghijklmnopqrstuvwx"
	var out bytes.Buffer
	env := map[string]string{"OLLAMA_API_KEY": key, "OLLAMA_MODEL": "m"}
	if code := report(env, &out, &fakeLister{models: []string{"m"}}); code != 0 {
		t.Fatalf("code = %d, output = %q", code, out.String())
	}
	if strings.Contains(out.String(), "hint") {
		t.Fatalf("a well-formed key should not be questioned: %q", out.String())
	}
	if !strings.Contains(out.String(), "set (57 characters)") {
		t.Fatalf("output = %q", out.String())
	}
}
