package main

import (
	"io"
	"strings"
	"testing"
)

func TestParseMemory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "bare bytes", input: "1024", want: 1024},
		{name: "kilo", input: "1K", want: 1 << 10},
		{name: "kib", input: "1KiB", want: 1 << 10},
		{name: "mega lower case", input: "128m", want: 128 << 20},
		{name: "mib", input: "256MiB", want: 256 << 20},
		{name: "giga", input: "2G", want: 2 << 30},
		{name: "gib with spaces", input: "  1 GiB  ", want: 1 << 30},
		{name: "zero", input: "0", want: 0},
		{name: "empty", input: "", wantErr: true},
		{name: "no digits", input: "M", wantErr: true},
		{name: "unknown unit", input: "10XB", wantErr: true},
		{name: "negative", input: "-1M", wantErr: true},
		{name: "float", input: "1.5G", wantErr: true},
		{name: "overflow", input: "99999999999999999999T", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseMemory(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMemory(%q) = %d, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMemory(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMemory(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseCPUs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "integer", input: "1", want: 1},
		{name: "fraction", input: "0.5", want: 0.5},
		{name: "two point five", input: "2.5", want: 2.5},
		{name: "with spaces", input: " 4 ", want: 4},
		{name: "empty", input: "", wantErr: true},
		{name: "not a number", input: "many", wantErr: true},
		{name: "zero", input: "0", wantErr: true},
		{name: "negative", input: "-2", wantErr: true},
		{name: "infinity", input: "Inf", wantErr: true},
		{name: "nan", input: "NaN", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseCPUs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCPUs(%q) = %v, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCPUs(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseCPUs(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		want        Config
		wantHelp    bool
		wantErr     bool
		errContains string
	}{
		{
			name: "minimal",
			args: []string{"--rootfs", "/srv/rootfs", "--", "/bin/sh"},
			want: Config{Rootfs: "/srv/rootfs", Command: []string{"/bin/sh"}},
		},
		{
			name: "memory and cpus",
			args: []string{"--rootfs", "/srv/rootfs", "--memory", "256M", "--cpus", "1.5", "--", "/bin/sh", "-c", "true"},
			want: Config{
				Rootfs:  "/srv/rootfs",
				Memory:  256 << 20,
				CPUs:    1.5,
				Command: []string{"/bin/sh", "-c", "true"},
			},
		},
		{
			name: "flags after command are passed through",
			args: []string{"--rootfs", "/r", "--", "/bin/echo", "--memory", "1G"},
			want: Config{Rootfs: "/r", Command: []string{"/bin/echo", "--memory", "1G"}},
		},
		{
			name:     "help",
			args:     []string{"--help"},
			wantHelp: true,
		},
		{
			name:        "missing rootfs",
			args:        []string{"--", "/bin/sh"},
			wantErr:     true,
			errContains: "--rootfs is required",
		},
		{
			name:        "missing command",
			args:        []string{"--rootfs", "/r"},
			wantErr:     true,
			errContains: "no command given",
		},
		{
			name:        "bad memory",
			args:        []string{"--rootfs", "/r", "--memory", "lots", "--", "/bin/sh"},
			wantErr:     true,
			errContains: "--memory",
		},
		{
			name:        "bad cpus",
			args:        []string{"--rootfs", "/r", "--cpus", "0", "--", "/bin/sh"},
			wantErr:     true,
			errContains: "--cpus",
		},
		{
			name:    "unknown flag",
			args:    []string{"--rootfs", "/r", "--nope", "--", "/bin/sh"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseArgs(tt.args, io.Discard)
			if tt.wantHelp {
				if err == nil || !strings.Contains(err.Error(), "help") {
					t.Fatalf("ParseArgs(%v) error = %v, want help sentinel", tt.args, err)
				}
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseArgs(%v) = %+v, want error", tt.args, got)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("ParseArgs(%v) error = %q, want it to contain %q", tt.args, err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseArgs(%v) unexpected error: %v", tt.args, err)
			}
			if got.Rootfs != tt.want.Rootfs {
				t.Errorf("Rootfs = %q, want %q", got.Rootfs, tt.want.Rootfs)
			}
			if got.Memory != tt.want.Memory {
				t.Errorf("Memory = %d, want %d", got.Memory, tt.want.Memory)
			}
			if got.CPUs != tt.want.CPUs {
				t.Errorf("CPUs = %v, want %v", got.CPUs, tt.want.CPUs)
			}
			if strings.Join(got.Command, " ") != strings.Join(tt.want.Command, " ") {
				t.Errorf("Command = %v, want %v", got.Command, tt.want.Command)
			}
		})
	}
}
