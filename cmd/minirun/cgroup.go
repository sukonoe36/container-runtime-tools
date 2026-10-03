package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultCgroupRoot is the mount point of the cgroup v2 unified hierarchy.
const DefaultCgroupRoot = "/sys/fs/cgroup"

// minirunGroup is the parent cgroup directory created below the cgroup root.
const minirunGroup = "minirun"

// DefaultCPUPeriodMicros is the cgroup v2 default cpu.max period, in
// microseconds. Quotas are expressed relative to this window.
const DefaultCPUPeriodMicros int64 = 100000

// Limits describes the cgroup v2 resource controls applied to a sandbox.
type Limits struct {
	// MemoryMaxBytes is the hard memory limit in bytes; 0 means unlimited.
	MemoryMaxBytes int64
	// CPUQuotaMicros is the CPU time allowed per period, in microseconds;
	// 0 means unlimited.
	CPUQuotaMicros int64
	// CPUPeriodMicros is the length of the quota window in microseconds.
	// It falls back to DefaultCPUPeriodMicros when unset.
	CPUPeriodMicros int64
}

// LimitsFromConfig derives cgroup limits from a parsed configuration.
func LimitsFromConfig(cfg *Config) Limits {
	if cfg == nil {
		return Limits{}
	}

	limits := Limits{
		MemoryMaxBytes:  cfg.Memory,
		CPUPeriodMicros: DefaultCPUPeriodMicros,
	}
	if cfg.CPUs > 0 {
		limits.CPUQuotaMicros = CPUQuotaMicros(cfg.CPUs, limits.CPUPeriodMicros)
	}
	return limits
}

// CPUQuotaMicros converts a core count into a cgroup v2 cpu.max quota for the
// given period. The result is rounded to the nearest microsecond.
func CPUQuotaMicros(cores float64, periodMicros int64) int64 {
	if cores <= 0 || periodMicros <= 0 {
		return 0
	}
	return int64(cores*float64(periodMicros) + 0.5)
}

// MemoryMaxContent renders the value written to cgroup v2's memory.max.
func (l Limits) MemoryMaxContent() string {
	if l.MemoryMaxBytes <= 0 {
		return "max"
	}
	return strconv.FormatInt(l.MemoryMaxBytes, 10)
}

// CPUMaxContent renders the value written to cgroup v2's cpu.max, which is
// "<quota> <period>".
func (l Limits) CPUMaxContent() string {
	period := l.CPUPeriodMicros
	if period <= 0 {
		period = DefaultCPUPeriodMicros
	}
	if l.CPUQuotaMicros <= 0 {
		return "max " + strconv.FormatInt(period, 10)
	}
	return strconv.FormatInt(l.CPUQuotaMicros, 10) + " " + strconv.FormatInt(period, 10)
}

// IsCgroupV2 reports whether the unified hierarchy is mounted at root.
func IsCgroupV2(root string) bool {
	info, err := os.Stat(filepath.Join(root, "cgroup.controllers"))
	return err == nil && !info.IsDir()
}

// CgroupPath returns the cgroup directory for a sandbox ID below root.
func CgroupPath(root, id string) string {
	return filepath.Join(root, minirunGroup, id)
}

// writeCgroupFile writes value to name inside dir. It is deliberately separated
// from group creation so the content logic can be unit tested without root.
func writeCgroupFile(dir, name, value string) error {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// WriteLimits writes memory.max and cpu.max into dir.
func WriteLimits(dir string, limits Limits) error {
	if err := writeCgroupFile(dir, "memory.max", limits.MemoryMaxContent()); err != nil {
		return err
	}
	if err := writeCgroupFile(dir, "cpu.max", limits.CPUMaxContent()); err != nil {
		return err
	}
	return nil
}

// EnableControllers asks the cgroup at dir to hand the named controllers down
// to its children by writing to cgroup.subtree_control. Enabling a controller
// the parent does not own is rejected by the kernel, so this is best-effort and
// callers may log and continue.
func EnableControllers(dir string, controllers ...string) error {
	if len(controllers) == 0 {
		return errors.New("no controllers specified")
	}
	value := ""
	for _, c := range controllers {
		value += "+" + c + " "
	}
	return writeCgroupFile(dir, "cgroup.subtree_control", strings.TrimSpace(value))
}

// CreateGroup creates the parent and sandbox cgroups and applies limits.
// It returns the sandbox cgroup directory. Creating the sandbox cgroup can only
// succeed once the cpu and memory controllers are enabled in the parent, which
// cgroup v2 exposes as memory.max and cpu.max in the child.
func CreateGroup(root, id string, limits Limits) (string, error) {
	if !IsCgroupV2(root) {
		return "", fmt.Errorf("cgroup v2 unified hierarchy not found at %s", root)
	}
	if id == "" {
		return "", errors.New("empty sandbox id")
	}

	parent := filepath.Join(root, minirunGroup)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("creating parent cgroup %s: %w", parent, err)
	}

	// Best-effort: the caller may not be allowed to change the root's subtree
	// control, in which case only the parent's is used.
	if err := EnableControllers(root, "cpu", "memory"); err != nil {
		fmt.Fprintf(os.Stderr, "minirun: note: could not enable controllers at %s: %v\n", root, err)
	}
	if err := EnableControllers(parent, "cpu", "memory"); err != nil {
		fmt.Fprintf(os.Stderr, "minirun: note: could not enable controllers at %s: %v\n", parent, err)
	}

	dir := CgroupPath(root, id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating cgroup %s: %w", dir, err)
	}

	if err := WriteLimits(dir, limits); err != nil {
		// Clean up so a failed run does not leave a half-configured cgroup.
		_ = os.Remove(dir)
		return "", err
	}

	return dir, nil
}

// AddProcess moves pid into the cgroup at dir by writing to cgroup.procs.
func AddProcess(dir string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	if err := writeCgroupFile(dir, "cgroup.procs", strconv.Itoa(pid)); err != nil {
		return fmt.Errorf("adding pid %d to %s: %w", pid, dir, err)
	}
	return nil
}

// RemoveGroup deletes the cgroup at dir. The kernel refuses to remove a cgroup
// that still has processes in it, which is the desired safety behaviour.
func RemoveGroup(dir string) error {
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("removing cgroup %s: %w", dir, err)
	}
	return nil
}

