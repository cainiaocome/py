// Package repl is the line-based interface used when stdin or stdout is not a
// terminal. It mirrors the full-screen controller's commands and streaming,
// printing plain text so it stays pipe-friendly.
package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/cainiaocome/py/internal/command"
	"github.com/cainiaocome/py/internal/tui"
)

type streamResult struct {
	text string
	err  error
}

// Run reads prompts from in and writes replies to out until EOF or /exit.
func Run(c tui.Session, in io.Reader, out io.Writer) error {
	fmt.Fprintln(out, "AI Chat · Ollama Cloud · Model: "+c.ModelName())
	fmt.Fprintln(out, "Vim editing · Enter: send · Alt+Enter: newline · Tab: complete · "+command.Hint())
	fmt.Fprintln(out, "Web tools: disabled · "+command.EnableWeb)

	reader := bufio.NewReader(in)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	for {
		fmt.Fprint(out, "You ["+c.ModelName()+"] > ")
		line, readErr := reader.ReadString('\n')
		value := strings.TrimSpace(line)
		if value == "" {
			if readErr != nil {
				break
			}
			continue
		}

		spec, argument, known := command.Parse(value)
		switch {
		case !known && command.IsCommand(value):
			fmt.Fprintln(out, command.UnknownNotice())
		case !known:
			runResponse(c, out, signals, value)
		default:
			if err := runCommand(c, spec, argument, reader, out); err != nil {
				fmt.Fprintln(out, "Could not select a model. Try /model <name> or check your connection and key.")
			}
			if spec.Name == "/exit" {
				return nil
			}
		}

		if readErr != nil {
			break
		}
	}
	fmt.Fprintln(out, "Goodbye.")
	return nil
}

func runResponse(c tui.Session, out io.Writer, signals <-chan os.Signal, prompt string) {
	fmt.Fprintln(out, "Assistant ["+c.ModelName()+"]")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Drain a Ctrl+C queued while idle so it does not cancel this response.
	select {
	case <-signals:
	default:
	}

	done := make(chan streamResult, 1)
	go func() {
		text, err := c.Stream(ctx, prompt,
			func(status string) { fmt.Fprintln(out, status) },
			nil,
		)
		done <- streamResult{text: text, err: err}
	}()

	select {
	case <-signals:
		cancel()
		result := <-done
		if result.text != "" {
			fmt.Fprintln(out, result.text)
		}
		fmt.Fprintln(out, "Response interrupted.")
	case result := <-done:
		if result.text != "" {
			fmt.Fprintln(out, result.text)
		}
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			fmt.Fprintln(out, "Request failed. Check your connection, API key and model; try again.")
		}
	}
}

func selectModel(c tui.Session, reader *bufio.Reader, out io.Writer, argument string) error {
	if argument == "" {
		fmt.Fprintln(out, "Current model: "+c.ModelName())
		models, err := c.ListModels(context.Background())
		if err != nil {
			return err
		}
		for index, name := range models {
			marker := ""
			if name == c.ModelName() {
				marker = " (current)"
			}
			fmt.Fprintf(out, "  %d. %s%s\n", index+1, name, marker)
		}
		if len(models) == 0 {
			fmt.Fprintln(out, "No models returned. Enter a model name manually.")
		}
		fmt.Fprint(out, "Model number or name (Enter to cancel) > ")
		line, _ := reader.ReadString('\n')
		argument = strings.TrimSpace(line)
		if argument == "" {
			return nil
		}
		if isDecimal(argument) {
			index := decimalValue(argument) - 1
			if index < 0 || index >= len(models) {
				fmt.Fprintln(out, "Invalid model number. Model unchanged.")
				return nil
			}
			argument = models[index]
		}
	}
	if err := c.SwitchModel(argument); err != nil {
		return err
	}
	fmt.Fprintln(out, "Model: "+c.ModelName()+" · conversation retained")
	return nil
}

// runCommand executes a parsed command. The full-screen UI has its own
// implementations because it owns a transcript to scroll and entries to append;
// what the two share is the command table and this parsing, which is the part
// that used to drift.
func runCommand(c tui.Session, spec command.Spec, argument string, reader *bufio.Reader, out io.Writer) error {
	switch spec.Name {
	case "/exit":
		fmt.Fprintln(out, "Goodbye.")
	case command.EnableWeb:
		if c.EnableWeb() {
			fmt.Fprintln(out, "Web search and web fetch enabled for this session.")
		} else {
			fmt.Fprintln(out, "Web search and web fetch are already enabled.")
		}
	case "/clear":
		c.Clear()
		fmt.Fprintln(out, "Conversation cleared.")
	case "/start":
		fmt.Fprintln(out, "/start works in the full-screen interface.")
	case "/model":
		return selectModel(c, reader, out, argument)
	}
	return nil
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func decimalValue(s string) int {
	value := 0
	for _, r := range s {
		value = value*10 + int(r-'0')
	}
	return value
}
