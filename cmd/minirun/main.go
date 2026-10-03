package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// sandboxInitSentinel marks the internal re-exec used to enter the sandbox.
// The parent starts /proc/self/exe with this as the first argument so that
// namespace setup happens inside the freshly created namespaces.
const sandboxInitSentinel = "__minirun_sandbox_init__"

// shutdownGrace is how long we wait for the sandbox to exit after forwarding a
// signal before returning control to the caller.
const shutdownGrace = 5 * time.Second

// realMain runs the CLI and returns a process exit code.
func realMain(args []string, stdout, stderr *os.File) int {
	cfg, err := ParseArgs(args, stderr)
	if errors.Is(err, errHelp) {
		Usage(stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "minirun: %v\n", err)
		fmt.Fprintln(stderr, "Try 'minirun --help' for usage.")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	code, err := RunInSandbox(ctx, cfg, stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "minirun: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == sandboxInitSentinel {
		os.Exit(runSandboxInit(os.Args[2:]))
	}
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
}
