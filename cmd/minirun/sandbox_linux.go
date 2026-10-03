//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// sandboxHostname is set inside the sandbox's UTS namespace.
const sandboxHostname = "minirun"

// sandboxPath is the PATH exported to the sandboxed command. It is resolved
// inside the sandbox root, after chroot.
const sandboxPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// cloneFlags are the namespaces the sandbox gets.
func cloneFlags() uintptr {
	return uintptr(unix.CLONE_NEWPID | unix.CLONE_NEWUTS | unix.CLONE_NEWNS | unix.CLONE_NEWNET)
}

// RunInSandbox starts the configured command inside fresh namespaces and with
// cgroup v2 limits applied. It returns the sandbox exit code.
//
// The process is started by re-executing this binary with the internal
// sandboxInitSentinel argument: the clone(2) performed by exec.Cmd places the
// child directly into the new namespaces, so the child can safely chroot and
// mount /proc before exec'ing the user's command.
func RunInSandbox(ctx context.Context, cfg *Config, stdout, stderr io.Writer) (int, error) {
	if cfg == nil {
		return 1, errors.New("nil configuration")
	}
	if os.Geteuid() != 0 {
		return 1, errors.New("must run as root: creating namespaces and cgroups requires privileges")
	}

	absRootfs, err := filepath.Abs(cfg.Rootfs)
	if err != nil {
		return 1, fmt.Errorf("resolving rootfs %q: %w", cfg.Rootfs, err)
	}
	info, err := os.Stat(absRootfs)
	if err != nil {
		return 1, fmt.Errorf("rootfs %q: %w", absRootfs, err)
	}
	if !info.IsDir() {
		return 1, fmt.Errorf("rootfs %q is not a directory", absRootfs)
	}

	id := sandboxID()
	cgroupDir, err := CreateGroup(DefaultCgroupRoot, id, LimitsFromConfig(cfg))
	if err != nil {
		return 1, fmt.Errorf("setting up cgroup: %w", err)
	}
	defer func() {
		if rmErr := RemoveGroup(cgroupDir); rmErr != nil {
			fmt.Fprintf(stderr, "minirun: note: %v\n", rmErr)
		}
	}()

	self, err := os.Executable()
	if err != nil {
		return 1, fmt.Errorf("locating own binary: %w", err)
	}

	childArgs := append([]string{sandboxInitSentinel, "--rootfs", absRootfs, "--"}, cfg.Command...)
	cmd := exec.CommandContext(ctx, self, childArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	var nodename [65]byte
	copy(nodename[:], sandboxHostname)
	cmd.SysProcAttr = &unix.SysProcAttr{
		Cloneflags: cloneFlags(),
		Utsname:    &unix.Utsname{Nodename: nodename},
	}

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("starting sandbox: %w", err)
	}

	if err := AddProcess(cgroupDir, cmd.Process.Pid); err != nil {
		// The limits are not active, so stop the sandbox rather than run it
		// unbounded while reporting success.
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return 1, fmt.Errorf("applying cgroup limits: %w", err)
	}

	err = cmd.Wait()
	if err == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(unix.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exitErr.ExitCode(), nil
	}
	if ctx.Err() != nil {
		return 130, fmt.Errorf("sandbox interrupted: %w", ctx.Err())
	}
	return 1, fmt.Errorf("waiting for sandbox: %w", err)
}

// runSandboxInit is the child side of the re-exec. It runs with the sandbox
// namespaces already created, prepares the mount namespace and root, then
// execs the user's command. It returns the exit code to use.
func runSandboxInit(args []string) int {
	rootfs, command, err := parseSandboxInitArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "minirun: %v\n", err)
		return 2
	}

	if err := setupMounts(rootfs); err != nil {
		fmt.Fprintf(os.Stderr, "minirun: %v\n", err)
		return 1
	}
	if err := unix.Chroot(rootfs); err != nil {
		fmt.Fprintf(os.Stderr, "minirun: chroot %q: %v\n", rootfs, err)
		return 1
	}
	if err := unix.Chdir("/"); err != nil {
		fmt.Fprintf(os.Stderr, "minirun: chdir /: %v\n", err)
		return 1
	}
	if err := mountProc(); err != nil {
		// A missing /proc is not fatal for every workload, so warn and continue.
		fmt.Fprintf(os.Stderr, "minirun: warning: %v\n", err)
	}

	child := exec.Command(command[0], command[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Env = []string{
		"PATH=" + sandboxPath,
		"HOME=/root",
		"TERM=" + os.Getenv("TERM"),
	}

	if err := child.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "minirun: running %q: %v\n", command[0], err)
		return 127
	}
	return 0
}

// setupMounts detaches the mount namespace from the host and bind-mounts the
// rootfs onto itself so it can be used as a chroot target.
func setupMounts(rootfs string) error {
	if err := unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("making mounts private: %w", err)
	}
	if err := unix.Mount(rootfs, rootfs, "", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mounting rootfs %q: %w", rootfs, err)
	}
	return nil
}

// mountProc mounts a fresh procfs at /proc inside the (already chrooted) root.
func mountProc() error {
	if err := os.MkdirAll("/proc", 0o555); err != nil {
		return fmt.Errorf("creating /proc in sandbox: %w", err)
	}
	if err := unix.Mount("proc", "/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("mounting /proc in sandbox: %w", err)
	}
	return nil
}

// parseSandboxInitArgs reads the internal "--rootfs PATH -- COMMAND..." form.
func parseSandboxInitArgs(args []string) (string, []string, error) {
	var rootfs string
	rest := args

	for len(rest) > 0 {
		if rest[0] == "--rootfs" {
			if len(rest) < 2 {
				return "", nil, errors.New("internal error: --rootfs needs a value")
			}
			rootfs = rest[1]
			rest = rest[2:]
			continue
		}
		if rest[0] == "--" {
			rest = rest[1:]
			break
		}
		return "", nil, fmt.Errorf("internal error: unexpected argument %q", rest[0])
	}

	if rootfs == "" {
		return "", nil, errors.New("internal error: missing --rootfs")
	}
	if len(rest) == 0 {
		return "", nil, errors.New("internal error: missing command")
	}
	return rootfs, rest, nil
}

// sandboxID returns a unique, filesystem-safe identifier for this run.
func sandboxID() string {
	return strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

