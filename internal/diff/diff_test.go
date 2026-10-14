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
			name:     "empty input defaults to devShells.x86_64-linux.default",
			input:    "",
			expected: []string{"devShells.x86_64-linux.default"},
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
}
