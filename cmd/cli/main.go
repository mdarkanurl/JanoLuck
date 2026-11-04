package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	version       = "1.0.0"
	defaultAPIURL = "http://localhost:3000"
)

type options struct {
	apiURL  string
	help    bool
	version bool
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		printUsage()
		os.Exit(2)
	}

	if opts.version {
		fmt.Printf("janoluck %s\n", version)
		return
	}
	if opts.help {
		printUsage()
		return
	}

	c, err := newClient(opts.apiURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	program := tea.NewProgram(newAppModel(c, opts.apiURL), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`JanoLuck - an optimistic friend for your what-ifs.

Usage:
  janoluck [--api-url URL]

Running janoluck opens the terminal chat app. If you are not signed in,
it starts with the login screen.

Options:
  --api-url URL   API base URL (default http://localhost:3000,
                  or the JANOLUCK_API_URL environment variable)
  --version       Print the version
  -h, --help      Show this help
`)
}

func parseArgs(args []string) (options, error) {
	opts := options{
		apiURL: os.Getenv("JANOLUCK_API_URL"),
	}
	if opts.apiURL == "" {
		opts.apiURL = defaultAPIURL
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--api-url=":
			opts.apiURL = strings.TrimPrefix(arg, "--api-url=")
		case "--api-url":
			if i+1 >= len(args) {
				return options{}, fmt.Errorf("--api-url requires a value")
			}
			i++
			opts.apiURL = args[i]
		case "--version":
			opts.version = true
		case "--help", "-h":
			opts.help = true
		default:
			return options{}, fmt.Errorf("unknown argument %q", arg)
		}
	}

	apiURL, err := normalizeAPIURL(opts.apiURL)
	if err != nil {
		return options{}, err
	}
	opts.apiURL = apiURL

	return opts, nil
}

func normalizeAPIURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("API URL cannot be empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid API URL %q", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("API URL must use http or https")
	}

	return strings.TrimRight(raw, "/"), nil
}
