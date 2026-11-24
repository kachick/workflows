package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestComputeSchedules(t *testing.T) {
	// Expected values match Zlib.crc32("kachick/workflows") (IEEE CRC-32).
	got := computeSchedules("kachick/workflows")
	want := schedules{
		bumpFlakeLock: "29 5 1 * *",
		automerge:     "29 */6 * * *",
	}
	if got != want {
		t.Fatalf("computeSchedules(%q) = %+v, want %+v", "kachick/workflows", got, want)
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:     "default outputs valid yaml",
			args:     []string{"kachick/workflows"},
			wantCode: 0,
			wantStdout: "bump-flake-lock: '29 5 1 * *'\n" +
				"automerge: '29 */6 * * *'\n",
		},
		{
			name:       "single workflow bump-flake-lock outputs raw cron",
			args:       []string{"--workflow", "bump-flake-lock", "kachick/workflows"},
			wantCode:   0,
			wantStdout: "29 5 1 * *\n",
		},
		{
			name:       "single workflow automerge outputs raw cron",
			args:       []string{"--workflow", "automerge", "kachick/workflows"},
			wantCode:   0,
			wantStdout: "29 */6 * * *\n",
		},
		{
			name:       "missing repo argument fails",
			args:       nil,
			wantCode:   2,
			wantStderr: "exactly one <owner/repo> argument is required",
		},
		{
			name:       "extra arguments fail",
			args:       []string{"kachick/workflows", "extra"},
			wantCode:   2,
			wantStderr: "exactly one <owner/repo> argument is required",
		},
		{
			name:       "invalid repo without slash fails",
			args:       []string{"workflows"},
			wantCode:   1,
			wantStderr: "owner/repo format",
		},
		{
			name:       "invalid repo with empty owner fails",
			args:       []string{"/workflows"},
			wantCode:   1,
			wantStderr: "owner/repo format",
		},
		{
			name:       "invalid repo with empty name fails",
			args:       []string{"kachick/"},
			wantCode:   1,
			wantStderr: "owner/repo format",
		},
		{
			name:       "invalid repo with extra slash fails",
			args:       []string{"kachick/workflows/extra"},
			wantCode:   1,
			wantStderr: "owner/repo format",
		},
		{
			name:       "invalid repo with whitespace fails",
			args:       []string{"kachick/work flows"},
			wantCode:   1,
			wantStderr: "must not contain whitespace",
		},
		{
			name:       "unknown workflow fails",
			args:       []string{"--workflow", "unknown", "kachick/workflows"},
			wantCode:   1,
			wantStderr: "unknown workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			gotCode := run(tt.args, &stdout, &stderr)
			if gotCode != tt.wantCode {
				t.Fatalf("run(%v) exit code = %d, want %d (stderr: %s)", tt.args, gotCode, tt.wantCode, stderr.String())
			}
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout {
				t.Errorf("stdout =\n%q\nwant:\n%q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
