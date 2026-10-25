// Command ghreport writes a JSON summary of the open issues of GitHub
// repositories.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/kgugunava/vk-ims/service/internal/cli"
)

// Exit codes are part of the CLI contract.
const (
	exitOK    = 0 // every repository succeeded
	exitUsage = 2 // invalid arguments; stdout stays empty
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// run is main without the process around it, so tests can call it directly.
func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	_, err := cli.Parse(args, getenv)
	if errors.Is(err, cli.ErrHelp) {
		fmt.Fprint(stderr, cli.Usage)
		return exitOK
	}
	var usageErr *cli.UsageError
	if errors.As(err, &usageErr) {
		fmt.Fprintf(stderr, "ghreport: %s\n", usageErr.Message)
		return exitUsage
	}
	// Fetching and the report are added in the following stages.
	_ = stdout
	return exitOK
}
