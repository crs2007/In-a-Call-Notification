package darwin

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantOK   bool
	}{
		{"nil", nil, 0, false},
		{"plain error", errors.New("boom"), 0, false},
		{"exit error", &exitError{name: "x", code: 44}, 44, true},
		{"wrapped exit error", fmt.Errorf("ctx: %w", &exitError{name: "x", code: 3}), 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, ok := exitCode(tt.err)
			if code != tt.wantCode || ok != tt.wantOK {
				t.Errorf("exitCode = %d,%v; want %d,%v", code, ok, tt.wantCode, tt.wantOK)
			}
		})
	}
}

func TestRunCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs /bin/sh")
	}
	tests := []struct {
		name     string
		script   string
		wantOut  string
		wantErrS string // stderr
		wantCode int    // 0: no error expected
		wantErr  string
	}{
		{"streams kept apart", "echo out; echo noise >&2", "out\n", "noise\n", 0, ""},
		{"failure carries stderr", "echo partial; echo why >&2; exit 3", "partial\n", "why\n", 3, "/bin/sh: exit status 3: why"},
		{"failure falls back to stdout", "echo only-stdout; exit 5", "only-stdout\n", "", 5, "/bin/sh: exit status 5: only-stdout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errOut, err := runCommand("/bin/sh", "-c", tt.script)
			if string(out) != tt.wantOut || string(errOut) != tt.wantErrS {
				t.Errorf("stdout, stderr = %q, %q; want %q, %q", out, errOut, tt.wantOut, tt.wantErrS)
			}
			code, _ := exitCode(err)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (err %v)", code, tt.wantCode, err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}

	if _, _, err := runCommand("/nonexistent/tool"); err == nil {
		t.Error("a missing binary must be an error")
	} else if _, ok := exitCode(err); ok {
		t.Error("a missing binary is not an exit status")
	}
}
