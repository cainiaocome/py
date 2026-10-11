// Package command describes the slash commands the chat understands.
//
// It is the single source of truth shared by both front-ends: the full-screen
// UI renders this list as its completion palette and uses Parse to interpret
// input, and the line-based fallback prints the same list in its banner and in
// its unknown-command notice. Keeping the table here is what stops those three
// from drifting apart.
package command

import "strings"

// EnableWeb turns on both web tools for the session. The name is long, so the
// palette's Tab completion is the intended way to type it.
const EnableWeb = "/enable-web-search-and-web-fetch"

// Spec is one command.
type Spec struct {
	// Name is what the user types, including the leading slash.
	Name string
	// Description is the palette meta text.
	Description string
	// Argument is a hint for the argument, empty when the command takes none.
	Argument string
	// TakesArgument reports whether the command accepts an argument, such as
	// "/model gpt-oss". Commands that do not take one must be given alone.
	TakesArgument bool
}

// Specs lists every command in palette order.
var Specs = []Spec{
	{Name: "/start", Description: "Scroll to the start of the last response"},
	{Name: "/clear", Description: "Clear conversation and transcript"},
	{Name: "/exit", Description: "Exit the chat"},
	{Name: "/model", Description: "Show or switch the model", Argument: "<name>", TakesArgument: true},
	{Name: EnableWeb, Description: "Enable web search and fetch tools"},
}

// Names returns every command name in palette order.
func Names() []string {
	names := make([]string, 0, len(Specs))
	for _, spec := range Specs {
		names = append(names, spec.Name)
	}
	return names
}

// Descriptions maps each command name to its palette description.
func Descriptions() map[string]string {
	descriptions := make(map[string]string, len(Specs))
	for _, spec := range Specs {
		descriptions[spec.Name] = spec.Description
	}
	return descriptions
}

// List renders the commands as a comma-separated list, for the messages that
// tell the user what is available.
func List() string { return strings.Join(Names(), ", ") }

// Match returns the commands starting with prefix, in palette order. An empty
// prefix matches everything, which is what the palette shows for a bare "/".
func Match(prefix string) []string {
	var matches []string
	for _, spec := range Specs {
		if strings.HasPrefix(spec.Name, prefix) {
			matches = append(matches, spec.Name)
		}
	}
	return matches
}

// Parse interprets a submitted line. It reports the matching command and its
// argument; ok is false when the line is a prompt rather than a command, which
// includes an unknown slash command. Leading and trailing space is ignored.
func Parse(line string) (spec Spec, argument string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return Spec{}, "", false
	}
	name, argument := trimmed, ""
	if index := strings.IndexAny(trimmed, " \t"); index >= 0 {
		name, argument = trimmed[:index], strings.TrimSpace(trimmed[index+1:])
	}
	for _, candidate := range Specs {
		if candidate.Name != name {
			continue
		}
		// "/clear something" is a mistake, not an argument.
		if argument != "" && !candidate.TakesArgument {
			return candidate, "", true
		}
		return candidate, argument, true
	}
	return Spec{}, "", false
}

// IsCommand reports whether a line looks like a slash command at all, whether
// or not it is one we know.
func IsCommand(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "/")
}

// Hint lists the commands worth advertising in a banner line. The long web
// tools name is left out because it gets its own line and the palette can
// complete it, but it is still part of Names.
func Hint() string {
	var short []string
	for _, spec := range Specs {
		if spec.Name == EnableWeb {
			continue
		}
		short = append(short, spec.Name)
	}
	return strings.Join(short, " · ")
}

// UnknownNotice is the message shown for a slash command we do not recognise.
func UnknownNotice() string { return "Unknown command. Available: " + List() }
