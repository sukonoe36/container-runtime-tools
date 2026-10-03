//go:build !linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// errNotLinux is returned by the non-Linux stubs. minirun depends on Linux
// namespaces and cgroup v2, neither of which exists on other platforms.
var errNotLinux = errors.New("minirun requires Linux: PID/UTS/mount/network namespaces and cgroup v2 are Linux-only")

// RunInSandbox is a stub that always fails on non-Linux platforms so that the
// package still builds and `go vet` can run everywhere.
func RunInSandbox(_ context.Context, _ *Config, _, _ io.Writer) (int, error) {
	return 1, errNotLinux
}

// runSandboxInit is a stub for the internal re-exec entry point.
func runSandboxInit(_ []string) int {
	fmt.Fprintln(os.Stderr, "minirun:", errNotLinux)
	return 1
}
