package tui

import "strings"

// EnableWebCommand turns on both web tools for the session.
const EnableWebCommand = "/enable-web-search-and-web-fetch"

// Commands are the slash commands, in palette order.
var Commands = []string{"/start", "/clear", "/exit", "/model", EnableWebCommand}

// CommandDescriptions is the palette meta text for each command.
var CommandDescriptions = map[string]string{
	"/start":         "Scroll to the start of the last response",
	"/clear":         "Clear conversation and transcript",
	"/exit":          "Exit the chat",
	"/model":         "Show or switch the model",
	EnableWebCommand: "Enable web search and fetch tools",
}

// matchCommands returns the commands that start with prefix, in order.
func matchCommands(prefix string) []string {
	var matches []string
	for _, command := range Commands {
		if strings.HasPrefix(command, prefix) {
			matches = append(matches, command)
		}
	}
	return matches
}

// commandNames renders the available commands for the unknown-command notice.
func commandNames() string {
	return strings.Join(Commands, ", ")
}
