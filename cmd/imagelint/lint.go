// Package main implements imagelint: a Dockerfile linter that flags common
// container image hygiene problems (floating tags, root user, single-stage
// builds, missing health checks, and uncleaned package manager caches).
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Severity classifies a finding.
type Severity string

// Supported severities.
const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Rule identifiers emitted by the linter.
const (
	RuleLatestTag          = "latest-tag"
	RuleRunAsRoot          = "run-as-root"
	RuleNoMultiStage       = "no-multi-stage"
	RuleMissingHealthcheck = "missing-healthcheck"
	RuleUncleanedAptCache  = "uncleaned-apt-cache"
)

// Instruction is one logical Dockerfile instruction. Continuation lines are
// folded into a single instruction whose Line is the first physical line.
type Instruction struct {
	Line int
	Cmd  string
	Args string
	Raw  string
}

// Finding is a single lint result.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Line     int      `json:"line"`
	Message  string   `json:"message"`
}

// String renders a finding for human-readable output.
func (f Finding) String() string {
	return fmt.Sprintf("line %d: [%s] %s (%s)", f.Line, f.Severity, f.Message, f.Rule)
}

// ParseDockerfile reads a Dockerfile and returns its logical instructions.
// Comment lines and blank lines are skipped; backslash continuations are
// joined, and the reported line number is that of the first physical line.
func ParseDockerfile(r io.Reader) ([]Instruction, error) {
	if r == nil {
		return nil, errors.New("nil reader")
	}

	var (
		instructions []Instruction
		scanner      = bufio.NewScanner(r)
		pending      strings.Builder
		startLine    int
		lineNo       int
	)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()

		trimmed := strings.TrimSpace(raw)
		if pending.Len() == 0 {
			// Skip blank lines and full-line comments.
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			startLine = lineNo
		}

		// Fold continuation lines together.
		if strings.HasSuffix(trimmed, "\\") {
			pending.WriteString(strings.TrimSuffix(trimmed, "\\"))
			pending.WriteString(" ")
			continue
		}
		pending.WriteString(trimmed)

		logical := strings.TrimSpace(pending.String())
		pending.Reset()
		if logical == "" {
			continue
		}

		cmd, args := splitInstruction(logical)
		instructions = append(instructions, Instruction{
			Line: startLine,
			Cmd:  cmd,
			Args: args,
			Raw:  logical,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading Dockerfile: %w", err)
	}

	// A trailing continuation line still counts as an instruction.
	if logical := strings.TrimSpace(pending.String()); logical != "" {
		cmd, args := splitInstruction(logical)
		instructions = append(instructions, Instruction{
			Line: startLine,
			Cmd:  cmd,
			Args: args,
			Raw:  logical,
		})
	}

	return instructions, nil
}

// splitInstruction splits "FROM alpine:3.19 AS build" into "FROM" and the rest.
func splitInstruction(line string) (string, string) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return strings.ToUpper(line), ""
	}
	return strings.ToUpper(line[:idx]), strings.TrimSpace(line[idx+1:])
}

// Lint runs every rule over the parsed instructions and returns the findings
// in rule order.
func Lint(instructions []Instruction) []Finding {
	findings := make([]Finding, 0, len(instructions))
	findings = append(findings, lintLatestTag(instructions)...)
	findings = append(findings, lintRunAsRoot(instructions)...)
	findings = append(findings, lintMultiStage(instructions)...)
	findings = append(findings, lintHealthcheck(instructions)...)
	findings = append(findings, lintAptCache(instructions)...)
	return findings
}
