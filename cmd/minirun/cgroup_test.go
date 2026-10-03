package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryMaxContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits Limits
		want   string
	}{
		{name: "unlimited", limits: Limits{}, want: "max"},
		{name: "negative treated as unlimited", limits: Limits{MemoryMaxBytes: -1}, want: "max"},
		{name: "bytes", limits: Limits{MemoryMaxBytes: 268435456}, want: "268435456"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.limits.MemoryMaxContent(); got != tt.want {
				t.Fatalf("MemoryMaxContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCPUMaxContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		limits Limits
		want   string
	}{
		{
			name:   "unlimited falls back to default period",
			limits: Limits{},
			want:   "max 100000",
		},
		{
			name:   "quota with explicit period",
			limits: Limits{CPUQuotaMicros: 150000, CPUPeriodMicros: 100000},
			want:   "150000 100000",
		},
		{
			name:   "quota with missing period uses default",
			limits: Limits{CPUQuotaMicros: 50000},
			want:   "50000 100000",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.limits.CPUMaxContent(); got != tt.want {
				t.Fatalf("CPUMaxContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCPUQuotaMicros(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cores  float64
		period int64
		want   int64
	}{
		{name: "one core", cores: 1, period: 100000, want: 100000},
		{name: "half core", cores: 0.5, period: 100000, want: 50000},
		{name: "two and a half cores", cores: 2.5, period: 100000, want: 250000},
		{name: "rounds to nearest microsecond", cores: 0.3333, period: 100000, want: 33330},
		{name: "zero cores", cores: 0, period: 100000, want: 0},
		{name: "negative cores", cores: -1, period: 100000, want: 0},
		{name: "zero period", cores: 1, period: 0, want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := CPUQuotaMicros(tt.cores, tt.period); got != tt.want {
				t.Fatalf("CPUQuotaMicros(%v, %d) = %d, want %d", tt.cores, tt.period, got, tt.want)
			}
		})
	}
}

func TestLimitsFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *Config
		want Limits
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: Limits{},
		},
		{
			name: "memory only",
			cfg:  &Config{Memory: 128 << 20},
			want: Limits{MemoryMaxBytes: 128 << 20, CPUPeriodMicros: DefaultCPUPeriodMicros},
		},
		{
			name: "cpus only",
			cfg:  &Config{CPUs: 2},
			want: Limits{CPUQuotaMicros: 200000, CPUPeriodMicros: DefaultCPUPeriodMicros},
		},
		{
			name: "both",
			cfg:  &Config{Memory: 64 << 20, CPUs: 0.25},
			want: Limits{
				MemoryMaxBytes:  64 << 20,
				CPUQuotaMicros:  25000,
				CPUPeriodMicros: DefaultCPUPeriodMicros,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := LimitsFromConfig(tt.cfg); got != tt.want {
				t.Fatalf("LimitsFromConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCgroupPath(t *testing.T) {
	t.Parallel()

	got := CgroupPath("/sys/fs/cgroup", "123-abc")
	want := filepath.Join("/sys/fs/cgroup", "minirun", "123-abc")
	if got != want {
		t.Fatalf("CgroupPath() = %q, want %q", got, want)
	}
}

func TestWriteLimits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	limits := Limits{
		MemoryMaxBytes:  512 << 20,
		CPUQuotaMicros:  150000,
		CPUPeriodMicros: 100000,
	}

	if err := WriteLimits(dir, limits); err != nil {
		t.Fatalf("WriteLimits() unexpected error: %v", err)
	}

	tests := []struct {
		file string
		want string
	}{
		{file: "memory.max", want: "536870912"},
		{file: "cpu.max", want: "150000 100000"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			if string(data) != tt.want {
				t.Fatalf("%s = %q, want %q", tt.file, string(data), tt.want)
			}
		})
	}
}

func TestWriteLimitsUnlimited(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := WriteLimits(dir, Limits{}); err != nil {
		t.Fatalf("WriteLimits() unexpected error: %v", err)
	}

	memoryMax, err := os.ReadFile(filepath.Join(dir, "memory.max"))
	if err != nil {
		t.Fatalf("reading memory.max: %v", err)
	}
	if string(memoryMax) != "max" {
		t.Fatalf("memory.max = %q, want %q", string(memoryMax), "max")
	}

	cpuMax, err := os.ReadFile(filepath.Join(dir, "cpu.max"))
	if err != nil {
		t.Fatalf("reading cpu.max: %v", err)
	}
	if string(cpuMax) != "max 100000" {
		t.Fatalf("cpu.max = %q, want %q", string(cpuMax), "max 100000")
	}
}

func TestWriteLimitsMissingDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "does-not-exist")
	if err := WriteLimits(dir, Limits{}); err == nil {
		t.Fatal("WriteLimits() on a missing directory = nil, want error")
	}
}

func TestEnableControllers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		controllers []string
		want        string
		wantErr     bool
	}{
		{name: "cpu and memory", controllers: []string{"cpu", "memory"}, want: "+cpu +memory"},
		{name: "single", controllers: []string{"memory"}, want: "+memory"},
		{name: "none", controllers: nil, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			err := EnableControllers(dir, tt.controllers...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("EnableControllers() = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("EnableControllers() unexpected error: %v", err)
			}

			data, err := os.ReadFile(filepath.Join(dir, "cgroup.subtree_control"))
			if err != nil {
				t.Fatalf("reading cgroup.subtree_control: %v", err)
			}
			if string(data) != tt.want {
				t.Fatalf("cgroup.subtree_control = %q, want %q", string(data), tt.want)
			}
		})
	}
}

func TestIsCgroupV2(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if IsCgroupV2(dir) {
		t.Fatal("IsCgroupV2() on an empty directory = true, want false")
	}

	if err := os.WriteFile(filepath.Join(dir, "cgroup.controllers"), []byte("cpu memory\n"), 0o644); err != nil {
		t.Fatalf("writing cgroup.controllers: %v", err)
	}
	if !IsCgroupV2(dir) {
		t.Fatal("IsCgroupV2() with cgroup.controllers present = false, want true")
	}
}

func TestAddProcessRejectsBadPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := AddProcess(dir, 0); err == nil {
		t.Fatal("AddProcess(0) = nil, want error")
	}
	if err := AddProcess(dir, -5); err == nil {
		t.Fatal("AddProcess(-5) = nil, want error")
	}
}

func TestAddProcessWritesPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := AddProcess(dir, 4321); err != nil {
		t.Fatalf("AddProcess() unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		t.Fatalf("reading cgroup.procs: %v", err)
	}
	if string(data) != "4321" {
		t.Fatalf("cgroup.procs = %q, want %q", string(data), "4321")
	}
}

