// Command pychat is a minimal streaming terminal chat client for Ollama Cloud.
package main

import (
	"fmt"
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
var Version = "0.1.0"

func main() {
	if handled := handleArguments(os.Args[1:]); handled {
		return
	}

	env := config.Environ()
	if err := config.Load(env, "", ""); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		os.Exit(1)
	}
	apiKey := strings.TrimSpace(env["OLLAMA_API_KEY"])
	model := strings.TrimSpace(env["OLLAMA_MODEL"])
	var missing []string
	if apiKey == "" {
		missing = append(missing, "OLLAMA_API_KEY")
	}
	if model == "" {
		missing = append(missing, "OLLAMA_MODEL")
	}
	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "ERROR: Missing required environment variables: "+strings.Join(missing, ", "))
		os.Exit(1)
	}

	client := chat.New(apiKey, model)
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

// handleArguments emulates argparse's --version/--help while ignoring anything
// else, so embedding never trips over a foreign command line.
func handleArguments(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--version":
			fmt.Printf("pychat %s\n", Version)
			return true
		case "--help", "-h":
			printUsage()
			return true
		}
	}
	return false
}

func printUsage() {
	fmt.Println("usage: pychat [-h] [--version]")
	fmt.Println()
	fmt.Println("Minimal streaming terminal chat with Ollama Cloud.")
	fmt.Println()
	fmt.Println("options:")
	fmt.Println("  -h, --help  show this help message and exit")
	fmt.Println("  --version   show program's version number and exit")
}
