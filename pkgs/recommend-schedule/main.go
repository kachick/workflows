package main

import (
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"
)

type schedules struct {
	bumpFlakeLock string
	automerge     string
}

func validateRepo(repo string) error {
	if strings.ContainsAny(repo, " \t\r\n") {
		return fmt.Errorf("repository %q must not contain whitespace", repo)
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("repository %q must be in owner/repo format", repo)
	}
	return nil
}

func computeSchedules(repo string) schedules {
	// Use IEEE CRC-32 so results stay identical to the previous Zlib.crc32 helper.
	crc := crc32.ChecksumIEEE([]byte(repo))
	minute := crc % 60
	hour := (crc / 60) % 6
	return schedules{
		bumpFlakeLock: fmt.Sprintf("%d %d 1 * *", minute, hour),
		automerge:     fmt.Sprintf("%d */6 * * *", minute),
	}
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("recommend-schedule", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workflow := fs.String("workflow", "", "Output raw cron expression for a single workflow (bump-flake-lock or automerge)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: recommend-schedule [--workflow bump-flake-lock|automerge] <owner/repo>\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "Error: exactly one <owner/repo> argument is required")
		fs.Usage()
		return 2
	}

	repo := fs.Arg(0)
	if err := validateRepo(repo); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	s := computeSchedules(repo)

	switch *workflow {
	case "":
		fmt.Fprintf(stdout, "bump-flake-lock: '%s'\n", s.bumpFlakeLock)
		fmt.Fprintf(stdout, "automerge: '%s'\n", s.automerge)
	case "bump-flake-lock":
		fmt.Fprintln(stdout, s.bumpFlakeLock)
	case "automerge":
		fmt.Fprintln(stdout, s.automerge)
	default:
		fmt.Fprintf(stderr, "Error: unknown workflow %q (expected bump-flake-lock or automerge)\n", *workflow)
		return 1
	}

	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
