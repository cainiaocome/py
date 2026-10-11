// Command gochat is a minimal streaming terminal chat client for Ollama Cloud.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/cainiaocome/py/internal/chat"
	"github.com/cainiaocome/py/internal/config"
	"github.com/cainiaocome/py/internal/repl"
	"github.com/cainiaocome/py/internal/tui"
)

// Version is overridable at build time:
//
//	go build -ldflags "-X main.Version=1.2.3"
var Version = "0.2.1"

// checkTimeout bounds --check so an unreachable endpoint reports instead of
// hanging.
const checkTimeout = 20 * time.Second

func main() {
	if handled := handleArguments(os.Args[1:], os.Stdout); handled {
		return
	}
	if slices.Contains(os.Args[1:], "--check") {
		os.Exit(runCheck(config.Environ(), os.Stdout))
	}

	env := config.Environ()
	missing, err := requireCredentials(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		os.Exit(1)
	}
	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "ERROR: Missing required environment variables: "+strings.Join(missing, ", "))
		os.Exit(1)
	}

	client := chat.New(strings.TrimSpace(env["OLLAMA_API_KEY"]), strings.TrimSpace(env["OLLAMA_MODEL"]))
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		if err := tui.Run(client); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
			os.Exit(1)
		}
		return
	}
	if err := repl.Run(client, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		os.Exit(1)
	}
}

// requireCredentials loads configuration into env and returns the credential
// names that are still missing. It returns an error when AI_CHAT_ENV_FILE names
// a file that does not exist.
func requireCredentials(env map[string]string) ([]string, error) {
	if err := config.Load(env, "", ""); err != nil {
		return nil, err
	}
	var missing []string
	if strings.TrimSpace(env["OLLAMA_API_KEY"]) == "" {
		missing = append(missing, "OLLAMA_API_KEY")
	}
	if strings.TrimSpace(env["OLLAMA_MODEL"]) == "" {
		missing = append(missing, "OLLAMA_MODEL")
	}
	return missing, nil
}

// handleArguments emulates argparse's --version/--help while ignoring anything
// else, so embedding never trips over a foreign command line.
func handleArguments(args []string, out io.Writer) bool {
	for _, arg := range args {
		switch arg {
		case "--version":
			fmt.Fprintf(out, "gochat %s\n", Version)
			return true
		case "--help", "-h":
			printUsage(out)
			return true
		}
	}
	return false
}

// checker is the part of the chat backend that --check exercises.
type checker interface {
	ListModels(ctx context.Context) ([]string, error)
	Ping(ctx context.Context) error
}

// runCheck loads the credentials and asks the endpoint what it offers.
func runCheck(env map[string]string, out io.Writer) int {
	if err := config.Load(env, "", ""); err != nil {
		fmt.Fprintln(out, "ERROR: "+err.Error())
		return 1
	}
	apiKey := strings.TrimSpace(env["OLLAMA_API_KEY"])
	model := strings.TrimSpace(env["OLLAMA_MODEL"])
	return report(env, out, chat.New(apiKey, model))
}

// report prints the effective configuration and then probes the endpoint. It
// exists because the full-screen UI owns the terminal: when a request fails
// there is nowhere to read the reason, and "check your connection" is not
// something a user can act on.
//
// The API key is never printed, only whether it is set and how long it is.
func report(env map[string]string, out io.Writer, backend checker) int {
	apiKey := strings.TrimSpace(env["OLLAMA_API_KEY"])
	model := strings.TrimSpace(env["OLLAMA_MODEL"])

	fmt.Fprintf(out, "gochat %s\n", Version)
	fmt.Fprintf(out, "endpoint  %s\n", chat.BaseURL)
	if model == "" {
		fmt.Fprintln(out, "model     unset")
	} else {
		fmt.Fprintf(out, "model     %s\n", model)
	}
	if apiKey == "" {
		fmt.Fprintln(out, "api key   unset")
	} else {
		fmt.Fprintf(out, "api key   set (%d characters)\n", len(apiKey))
	}
	if apiKey == "" || model == "" {
		fmt.Fprintln(out, "result    FAILED: set OLLAMA_API_KEY and OLLAMA_MODEL, then run again")
		return 1
	}
	// A credential that carries a quote, a shell variable or a space is usually
	// not a credential: it is the rest of a .env line that a shell would have
	// stripped. Say so before the network round trip, because the failure it
	// causes is a bare 401.
	for _, name := range config.CredentialNames {
		if hint := credentialHint(name, strings.TrimSpace(env[name])); hint != "" {
			fmt.Fprintf(out, "hint      %s\n", hint)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	models, err := backend.ListModels(ctx)
	if err != nil {
		fmt.Fprintf(out, "result    FAILED: could not list models: %s\n", err)
		return 1
	}
	fmt.Fprintf(out, "models    %d available\n", len(models))
	if !slices.Contains(models, model) {
		fmt.Fprintf(out, "result    FAILED: %s is not a model this endpoint offers\n", model)
		fmt.Fprintf(out, "hint      run gochat and use /model to pick one, for example %s\n", strings.Join(firstModels(models, 3), ", "))
		return 1
	}
	// Model listing does not authenticate, so ask for a real completion. It is
	// the same endpoint the app uses, which makes this answer conclusive.
	fmt.Fprintf(out, "checking  chat completion with %s\n", model)
	if err := backend.Ping(ctx); err != nil {
		fmt.Fprintf(out, "result    FAILED: %s\n", err)
		return 1
	}
	fmt.Fprintln(out, "result    OK: the endpoint answered and the API key works")
	return 0
}

// credentialHint describes a credential that looks like a parsing artifact. It
// never quotes the value.
func credentialHint(name, value string) string {
	switch {
	case strings.ContainsAny(value, " \t"):
		return name + " contains whitespace, which a trailing comment in the .env would produce"
	case strings.ContainsAny(value, "$`"):
		return name + " contains a shell variable reference; the container launcher expands it and this binary does not, so export the value instead"
	case strings.ContainsAny(value, "\"'\""):
		return name + " contains a quote, which usually means the .env quoting is unbalanced"
	}
	return ""
}

// firstModels lists up to limit model names for a hint.
func firstModels(models []string, limit int) []string {
	if len(models) < limit {
		return models
	}
	return models[:limit]
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "usage: gochat [-h] [--version] [--check]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Minimal streaming terminal chat with Ollama Cloud.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "options:")
	fmt.Fprintln(out, "  -h, --help  show this help message and exit")
	fmt.Fprintln(out, "  --version   show program's version number and exit")
	fmt.Fprintln(out, "  --check     print the effective configuration and test the endpoint")
}
