// Command gochat is a minimal streaming terminal chat client for Ollama Cloud.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/cainiaocome/py/internal/chat"
	"github.com/cainiaocome/py/internal/config"
	"github.com/cainiaocome/py/internal/repl"
	"github.com/cainiaocome/py/internal/tui"
)

// Version is overridable at build time:
//
//	go build -ldflags "-X main.Version=1.2.3"
var Version = "0.1.9"

func main() {
	if handled := handleArguments(os.Args[1:], os.Stdout); handled {
		return
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

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "usage: gochat [-h] [--version]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Minimal streaming terminal chat with Ollama Cloud.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "options:")
	fmt.Fprintln(out, "  -h, --help  show this help message and exit")
	fmt.Fprintln(out, "  --version   show program's version number and exit")
}
