// Command imagelint scans a Dockerfile and reports common container image
// hygiene problems. It supports human-readable and JSON output.
//
// Usage:
//
//	imagelint [--file PATH] [--json] [--exit-zero]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// exitFinding is the exit code used when at least one finding was reported.
const exitFinding = 1

// exitError is the exit code used for usage or I/O errors.
const exitError = 2

// report is the JSON document emitted with --json.
type report struct {
	File     string        `json:"file"`
	Findings []Finding     `json:"findings"`
	Summary  reportSummary `json:"summary"`
}

// reportSummary counts findings by severity.
type reportSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Total    int `json:"total"`
}

// usage writes the imagelint help text to w.
func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: imagelint [options]

Scan a Dockerfile for container image hygiene problems.

Options:
  --file PATH    Dockerfile to scan, or "-" for stdin (default: Dockerfile)
  --json         Emit a JSON report instead of text
  --exit-zero    Always exit 0, even when findings are reported
  --help         Show this help and exit

Rules:
  latest-tag            base image is unpinned or uses the "latest" tag
  run-as-root           no USER instruction, or USER resolves to root
  no-multi-stage        single-stage build
  missing-healthcheck   no HEALTHCHECK instruction (or HEALTHCHECK NONE)
  uncleaned-apt-cache   apt install without removing /var/lib/apt/lists

Exit codes:
  0  no findings (or --exit-zero)
  1  findings reported
  2  usage or I/O error
`)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes imagelint and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("imagelint", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		path     = fs.String("file", "Dockerfile", "Dockerfile to scan, or - for stdin")
		asJSON   = fs.Bool("json", false, "emit JSON")
		exitZero = fs.Bool("exit-zero", false, "always exit 0")
		showHelp = fs.Bool("help", false, "show help")
	)

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "imagelint: %v\n", err)
		fmt.Fprintln(stderr, "Try 'imagelint --help' for usage.")
		return exitError
	}
	if *showHelp {
		usage(stdout)
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "imagelint: unexpected argument %q\n", fs.Arg(0))
		fmt.Fprintln(stderr, "Try 'imagelint --help' for usage.")
		return exitError
	}

	reader, closeFn, err := openInput(*path)
	if err != nil {
		fmt.Fprintf(stderr, "imagelint: %v\n", err)
		return exitError
	}
	defer closeFn()

	instructions, err := ParseDockerfile(reader)
	if err != nil {
		fmt.Fprintf(stderr, "imagelint: %v\n", err)
		return exitError
	}

	findings := Lint(instructions)

	if *asJSON {
		if err := writeJSON(stdout, *path, findings); err != nil {
			fmt.Fprintf(stderr, "imagelint: %v\n", err)
			return exitError
		}
	} else {
		writeText(stdout, *path, findings)
	}

	if len(findings) > 0 && !*exitZero {
		return exitFinding
	}
	return 0
}

// openInput returns a reader for the Dockerfile, or stdin when path is "-".
func openInput(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, func() {}, fmt.Errorf("opening %s: %w", path, err)
	}
	return file, func() { _ = file.Close() }, nil
}

// writeText renders findings for humans.
func writeText(w io.Writer, path string, findings []Finding) {
	if len(findings) == 0 {
		fmt.Fprintf(w, "%s: no issues found\n", path)
		return
	}

	fmt.Fprintf(w, "%s: %d issue(s) found\n", path, len(findings))
	for _, f := range findings {
		fmt.Fprintf(w, "  %s\n", f)
	}
}

// writeJSON renders findings as a JSON document.
func writeJSON(w io.Writer, path string, findings []Finding) error {
	if findings == nil {
		findings = []Finding{}
	}

	doc := report{File: path, Findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case SeverityError:
			doc.Summary.Errors++
		default:
			doc.Summary.Warnings++
		}
	}
	doc.Summary.Total = len(findings)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encoding JSON report: %w", err)
	}
	return nil
}
