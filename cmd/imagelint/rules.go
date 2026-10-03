package main

import (
	"fmt"
	"regexp"
	"strings"
)

// aptInstallPattern matches "apt install", "apt-get install" and variants with
// extra whitespace between the command and the subcommand.
var aptInstallPattern = regexp.MustCompile(`\bapt(-get)?\s+install\b`)

// aptCacheCleanupPattern matches the usual cleanup of the apt package lists.
var aptCacheCleanupPattern = regexp.MustCompile(`rm\s+-[a-zA-Z]*r[a-zA-Z]*f?[a-zA-Z]*\s+/var/lib/apt/lists`)

// lintLatestTag flags FROM instructions whose image is unpinned or pinned to
// the floating "latest" tag. Digest-pinned references and "scratch" are fine.
func lintLatestTag(instructions []Instruction) []Finding {
	var findings []Finding

	for _, inst := range instructions {
		if inst.Cmd != "FROM" {
			continue
		}

		ref := fromImageRef(inst.Args)
		switch {
		case ref == "", ref == "scratch":
			continue
		case strings.HasPrefix(ref, "$"):
			// ARG-provided image names cannot be resolved statically.
			continue
		case strings.Contains(ref, "@sha256:"):
			continue
		}

		tag := imageTag(ref)
		if tag == "" || tag == "latest" {
			findings = append(findings, Finding{
				Rule:     RuleLatestTag,
				Severity: SeverityWarning,
				Line:     inst.Line,
				Message: fmt.Sprintf(
					"base image %q is not pinned; use an explicit version tag or @sha256 digest", ref),
			})
		}
	}

	return findings
}

// fromImageRef extracts the image reference from FROM arguments, dropping any
// "AS <stage>" suffix and the platform flag.
func fromImageRef(args string) string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return ""
	}

	// Handle "FROM --platform=linux/amd64 alpine:3.19".
	if strings.HasPrefix(fields[0], "--") {
		fields = fields[1:]
		if len(fields) == 0 {
			return ""
		}
	}

	return fields[0]
}

// imageTag returns the tag portion of an image reference, or "" when the
// reference has no tag. A colon inside a registry host:port is not a tag.
func imageTag(ref string) string {
	colon := strings.LastIndex(ref, ":")
	if colon < 0 {
		return ""
	}
	if slash := strings.LastIndex(ref, "/"); slash > colon {
		return ""
	}
	return ref[colon+1:]
}

// lintRunAsRoot flags images that never drop privileges, or that explicitly run
// as root.
func lintRunAsRoot(instructions []Instruction) []Finding {
	var findings []Finding
	firstLine := firstInstructionLine(instructions)

	for _, inst := range instructions {
		if inst.Cmd != "USER" {
			continue
		}

		user := strings.TrimSpace(inst.Args)
		if user == "root" || user == "0" || strings.HasPrefix(user, "0:") {
			findings = append(findings, Finding{
				Rule:     RuleRunAsRoot,
				Severity: SeverityWarning,
				Line:     inst.Line,
				Message:  fmt.Sprintf("container runs as %q; create and switch to an unprivileged user", user),
			})
		}
		return findings
	}

	findings = append(findings, Finding{
		Rule:     RuleRunAsRoot,
		Severity: SeverityWarning,
		Line:     firstLine,
		Message:  "no USER instruction; the container will run as root by default",
	})
	return findings
}

// lintMultiStage flags Dockerfiles that build and ship in a single stage.
func lintMultiStage(instructions []Instruction) []Finding {
	fromCount := 0
	firstLine := firstInstructionLine(instructions)

	for _, inst := range instructions {
		if inst.Cmd == "FROM" {
			fromCount++
		}
	}

	if fromCount >= 2 {
		return nil
	}

	return []Finding{{
		Rule:     RuleNoMultiStage,
		Severity: SeverityWarning,
		Line:     firstLine,
		Message:  "single-stage build; a multi-stage build keeps build tooling out of the final image",
	}}
}

// lintHealthcheck flags images without a HEALTHCHECK instruction.
func lintHealthcheck(instructions []Instruction) []Finding {
	firstLine := firstInstructionLine(instructions)

	for _, inst := range instructions {
		if inst.Cmd != "HEALTHCHECK" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(inst.Args), "NONE") {
			return []Finding{{
				Rule:     RuleMissingHealthcheck,
				Severity: SeverityWarning,
				Line:     inst.Line,
				Message:  "HEALTHCHECK NONE disables container health reporting",
			}}
		}
		return nil
	}

	return []Finding{{
		Rule:     RuleMissingHealthcheck,
		Severity: SeverityWarning,
		Line:     firstLine,
		Message:  "no HEALTHCHECK instruction; the runtime cannot tell whether the container is healthy",
	}}
}

// lintAptCache flags apt installs that leave the package lists in the layer.
func lintAptCache(instructions []Instruction) []Finding {
	var findings []Finding

	for _, inst := range instructions {
		if inst.Cmd != "RUN" {
			continue
		}
		if !aptInstallPattern.MatchString(inst.Args) {
			continue
		}
		if aptCacheCleanupPattern.MatchString(inst.Args) {
			continue
		}

		findings = append(findings, Finding{
			Rule:     RuleUncleanedAptCache,
			Severity: SeverityWarning,
			Line:     inst.Line,
			Message:  "apt install without cleaning /var/lib/apt/lists; the package lists stay in the image layer",
		})
	}

	return findings
}

// firstInstructionLine returns the line of the first instruction, or 1 when the
// Dockerfile is empty.
func firstInstructionLine(instructions []Instruction) int {
	if len(instructions) == 0 {
		return 1
	}
	return instructions[0].Line
}
