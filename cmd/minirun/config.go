// Package main implements minirun: a small, dependency-light sandbox runner
// that starts a command inside PID, UTS, mount and network namespaces with
// cgroup v2 memory and CPU limits.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// errHelp is returned by ParseArgs when the caller asked for --help.
var errHelp = errors.New("help requested")

// Config is a parsed minirun invocation.
type Config struct {
	// Rootfs is the directory used as the sandbox root. Required.
	Rootfs string
	// Memory is the memory limit in bytes; 0 means "no limit".
	Memory int64
	// CPUs is the CPU limit in cores; 0 means "no limit".
	CPUs float64
	// Command is the command and arguments to run inside the sandbox.
	Command []string
}

// Usage writes the minirun help text to w.
func Usage(w io.Writer) {
	fmt.Fprint(w, `Usage: minirun [options] -- COMMAND [ARGS...]

Run COMMAND inside a sandbox with its own PID, UTS, mount and network
namespaces, optionally limited by cgroup v2 memory and CPU controls.

Options:
  --rootfs PATH   Directory to use as the sandbox root (required)
  --memory SIZE   Memory limit, e.g. 128M, 512MiB, 1G (default: unlimited)
  --cpus N        CPU limit in cores, e.g. 0.5, 1, 2.5 (default: unlimited)
  --help          Show this help and exit

Requirements:
  Linux, root (or CAP_SYS_ADMIN plus CAP_BPF-free cgroup delegation), a
  cgroup v2 unified hierarchy, and a populated rootfs.

Examples:
  sudo minirun --rootfs /var/lib/minirun/rootfs --memory 256M --cpus 1.5 -- /bin/sh
  sudo minirun --rootfs ./rootfs -- /usr/bin/env
`)
}

// ParseArgs parses minirun's command line. Diagnostics are written to stderr;
// errHelp is returned when --help was requested so the caller can exit 0.
func ParseArgs(args []string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("minirun", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render usage ourselves

	var (
		rootfs   = fs.String("rootfs", "", "sandbox root directory")
		memory   = fs.String("memory", "", "memory limit (e.g. 256M)")
		cpus     = fs.String("cpus", "", "CPU limit in cores (e.g. 1.5)")
		showHelp = fs.Bool("help", false, "show help")
	)

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parsing flags: %w", err)
	}
	if *showHelp {
		return nil, errHelp
	}

	cfg := &Config{Rootfs: *rootfs}

	if cfg.Rootfs == "" {
		return nil, errors.New("--rootfs is required")
	}

	if *memory != "" {
		bytes, err := ParseMemory(*memory)
		if err != nil {
			return nil, fmt.Errorf("--memory: %w", err)
		}
		cfg.Memory = bytes
	}

	if *cpus != "" {
		cores, err := ParseCPUs(*cpus)
		if err != nil {
			return nil, fmt.Errorf("--cpus: %w", err)
		}
		cfg.CPUs = cores
	}

	cfg.Command = fs.Args()
	if len(cfg.Command) == 0 {
		return nil, errors.New("no command given (use '-- COMMAND [ARGS...]')")
	}

	return cfg, nil
}

// memoryUnits maps a (lower-cased, whitespace-trimmed) suffix to a multiplier.
var memoryUnits = map[string]int64{
	"":    1,
	"b":   1,
	"k":   1 << 10,
	"kb":  1 << 10,
	"ki":  1 << 10,
	"kib": 1 << 10,
	"m":   1 << 20,
	"mb":  1 << 20,
	"mi":  1 << 20,
	"mib": 1 << 20,
	"g":   1 << 30,
	"gb":  1 << 30,
	"gi":  1 << 30,
	"gib": 1 << 30,
	"t":   1 << 40,
	"tb":  1 << 40,
	"ti":  1 << 40,
	"tib": 1 << 40,
}

// ParseMemory converts a human-readable size such as "256M" or "1GiB" into a
// byte count. Suffixes are binary (1K == 1024) and case-insensitive; a bare
// number is interpreted as bytes.
func ParseMemory(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("empty value")
	}

	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0, fmt.Errorf("invalid size %q: must start with a number", value)
	}

	amount, err := strconv.ParseInt(trimmed[:digits], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", value, err)
	}

	suffix := strings.ToLower(strings.TrimSpace(trimmed[digits:]))
	multiplier, ok := memoryUnits[suffix]
	if !ok {
		return 0, fmt.Errorf("invalid size %q: unknown unit %q", value, trimmed[digits:])
	}

	if amount != 0 && amount > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("invalid size %q: value overflows int64", value)
	}
	if amount < 0 {
		return 0, fmt.Errorf("invalid size %q: must not be negative", value)
	}

	return amount * multiplier, nil
}

// ParseCPUs converts a core count such as "1" or "1.5" into a float64.
// The value must be greater than zero.
func ParseCPUs(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("empty value")
	}

	cores, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid core count %q: %w", value, err)
	}
	if math.IsNaN(cores) || math.IsInf(cores, 0) {
		return 0, fmt.Errorf("invalid core count %q: not a finite number", value)
	}
	if cores <= 0 {
		return 0, fmt.Errorf("invalid core count %q: must be greater than zero", value)
	}

	return cores, nil
}
