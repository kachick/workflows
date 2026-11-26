package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestComputeSchedulesRangeInvariants(t *testing.T) {
	repos := []string{
		"kachick/workflows",
		"kachick/dotfiles",
		"kachick/anylang-template",
		"owner/a",
		"owner/zzzzzzzz",
	}

	for _, repo := range repos {
		got := computeSchedules(repo)

		var bumpMin, bumpHour int
		if n, err := fmt.Sscanf(got.bumpFlakeLock, "%d %d 1 * *", &bumpMin, &bumpHour); err != nil || n != 2 {
			t.Fatalf("unexpected bump-flake-lock format for %q: %q", repo, got.bumpFlakeLock)
		}
		if bumpMin < 0 || bumpMin >= 60 || bumpHour < 0 || bumpHour >= 6 {
			t.Errorf("bump-flake-lock out of range for %q: min=%d hour=%d", repo, bumpMin, bumpHour)
		}

		var autoMin int
		if n, err := fmt.Sscanf(got.automerge, "%d */6 * * *", &autoMin); err != nil || n != 1 {
			t.Fatalf("unexpected automerge format for %q: %q", repo, got.automerge)
		}
		if autoMin != bumpMin {
			t.Errorf("automerge minute (%d) != bump-flake-lock minute (%d) for %q", autoMin, bumpMin, repo)
		}
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
			name:       "help flag exits cleanly",
			args:       []string{"--help"},
			wantCode:   0,
			wantStderr: "Usage: recommend-schedule",
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
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout =\n%q\nwant:\n%q", stdout.String(), tt.wantStdout)
			}
			if tt.wantStderr == "" {
				if stderr.Len() != 0 {
					t.Errorf("unexpected stderr: %q", stderr.String())
				}
			} else if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
