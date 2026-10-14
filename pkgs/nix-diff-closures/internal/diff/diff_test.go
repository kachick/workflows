package diff

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type mockExecutor struct {
	responses map[string]string
	errors    map[string]error
}

func (m *mockExecutor) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmdKey := fmt.Sprintf("%s %v", name, args)
	if err, ok := m.errors[cmdKey]; ok {
		return "", err
	}
	if resp, ok := m.responses[cmdKey]; ok {
		return resp, nil
	}
	return "", fmt.Errorf("unexpected command: %s", cmdKey)
}

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty input returns empty slice",
			input:    "",
			expected: nil,
		},
		{
			name:     "single explicit target",
			input:    "devShells.x86_64-linux.default",
			expected: []string{"devShells.x86_64-linux.default"},
		},
		{
			name: "newline separated multiple targets with comments and prefix stripping",
			input: `
# dev shell
.#devShells.x86_64-linux.default
# nixos config
nixosConfigurations.desktop.config.system.build.toplevel
`,
			expected: []string{
				"devShells.x86_64-linux.default",
				"nixosConfigurations.desktop.config.system.build.toplevel",
			},
		},
		{
			name:  "comma separated targets",
			input: "packages.x86_64-linux.default, devShells.x86_64-linux.ci",
			expected: []string{
				"packages.x86_64-linux.default",
				"devShells.x86_64-linux.ci",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTargets(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("expected %#v, got %#v", tt.expected, got)
			}
		})
	}
}

func TestEngineRun(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]string{
			"nix [path-info --derivation .#devShells.x86_64-linux.default]":                             "/nix/store/111-drv",
			"nix [path-info --derivation git+file:///repo?rev=base-sha#devShells.x86_64-linux.default]": "/nix/store/222-drv",
			"nix [store diff-closures /nix/store/222-drv /nix/store/111-drv]":                           "bash: 5.2 → 5.3\n",
		},
	}

	engine := &Engine{
		Exec:    mock,
		RepoDir: "/repo",
	}

	t.Run("fails when targets is empty", func(t *testing.T) {
		_, err := engine.Run(context.Background(), "base-sha", "")
		if err == nil {
			t.Fatal("expected error when targets is empty")
		}
	})

	t.Run("succeeds with valid target", func(t *testing.T) {
		results, err := engine.Run(context.Background(), "base-sha", "devShells.x86_64-linux.default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}

		res := results[0]
		if res.Target != "devShells.x86_64-linux.default" {
			t.Fatalf("unexpected target: %s", res.Target)
		}
		if res.DiffText != "bash: 5.2 → 5.3\n" {
			t.Fatalf("unexpected diff text: %q", res.DiffText)
		}
		if res.NoChange {
			t.Fatal("expected NoChange = false")
		}
	})

	t.Run("escapes special characters in baseRev for URL query", func(t *testing.T) {
		mockSpecial := &mockExecutor{
			responses: map[string]string{
				"nix [path-info --derivation .#devShells.x86_64-linux.default]":                                "/nix/store/111-drv",
				"nix [path-info --derivation git+file:///repo?rev=topic%23123#devShells.x86_64-linux.default]": "/nix/store/222-drv",
				"nix [store diff-closures /nix/store/222-drv /nix/store/111-drv]":                              "bash: 5.2 → 5.3\n",
			},
		}
		engineSpecial := &Engine{
			Exec:    mockSpecial,
			RepoDir: "/repo",
		}

		results, err := engineSpecial.Run(context.Background(), "topic#123", "devShells.x86_64-linux.default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 || results[0].Error != "" {
			t.Fatalf("expected successful result, got %#v", results)
		}
		if results[0].BaseDrv != "/nix/store/222-drv" {
			t.Fatalf("expected BaseDrv = /nix/store/222-drv, got %q (diff: %q)", results[0].BaseDrv, results[0].DiffText)
		}
	})

	t.Run("normalizes closure diff text containing ANSI codes", func(t *testing.T) {
		mockANSI := &mockExecutor{
			responses: map[string]string{
				"nix [path-info --derivation .#devShells.x86_64-linux.default]":                             "/nix/store/111-drv",
				"nix [path-info --derivation git+file:///repo?rev=base-sha#devShells.x86_64-linux.default]": "/nix/store/222-drv",
				"nix [store diff-closures /nix/store/222-drv /nix/store/111-drv]":                           "pkg: \x1b[31;1m10.0 KiB\x1b[0m\n",
			},
		}
		engineANSI := &Engine{
			Exec:    mockANSI,
			RepoDir: "/repo",
		}

		results, err := engineANSI.Run(context.Background(), "base-sha", "devShells.x86_64-linux.default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 1 || results[0].Error != "" {
			t.Fatalf("expected successful result, got %#v", results)
		}
		expectedDiff := "pkg: +10.0 KiB\n"
		if results[0].DiffText != expectedDiff {
			t.Fatalf("expected DiffText = %q, got %q", expectedDiff, results[0].DiffText)
		}
	})
}

func TestNormalizeDiffOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text without ansi codes remains unchanged",
			input:    "bash: 5.2 → 5.3\n",
			expected: "bash: 5.2 → 5.3\n",
		},
		{
			name:     "strips ansi codes and normalizes positive size with plus sign",
			input:    "n4hz2f0rhvclfbgk4adkbxi6y8sd31cf: ε → ∅, \x1b[32;1m-199.8 MiB\x1b[0m\nrfcpg2ryywgma7wlswjzna99g9gq8laq: ∅ → ε, \x1b[31;1m199.8 MiB\x1b[0m\n",
			expected: "n4hz2f0rhvclfbgk4adkbxi6y8sd31cf: ε → ∅, -199.8 MiB\nrfcpg2ryywgma7wlswjzna99g9gq8laq: ∅ → ε, +199.8 MiB\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDiffOutput(tt.input)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
