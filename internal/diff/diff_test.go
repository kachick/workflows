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
		system   string
		expected []string
	}{
		{
			name:     "empty input defaults to devShells",
			input:    "",
			system:   "x86_64-linux",
			expected: []string{"devShells.x86_64-linux.default"},
		},
		{
			name:     "single target with system placeholder",
			input:    "devShells.${SYSTEM}.default",
			system:   "aarch64-linux",
			expected: []string{"devShells.aarch64-linux.default"},
		},
		{
			name: "newline separated multiple targets with comments and prefix stripping",
			input: `
# dev shell
.#devShells.${SYSTEM}.default
# nixos config
nixosConfigurations.desktop.config.system.build.toplevel
`,
			system: "x86_64-linux",
			expected: []string{
				"devShells.x86_64-linux.default",
				"nixosConfigurations.desktop.config.system.build.toplevel",
			},
		},
		{
			name:   "comma separated targets",
			input:  "packages.${SYSTEM}.default, devShells.${SYSTEM}.ci",
			system: "x86_64-linux",
			expected: []string{
				"packages.x86_64-linux.default",
				"devShells.x86_64-linux.ci",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTargets(tt.input, tt.system)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("expected %#v, got %#v", tt.expected, got)
			}
		})
	}
}

func TestEngineRun(t *testing.T) {
	mock := &mockExecutor{
		responses: map[string]string{
			"nix [eval --raw --impure --expr builtins.currentSystem]":                                   "x86_64-linux",
			"nix [path-info --derivation .#devShells.x86_64-linux.default]":                             "/nix/store/111-drv",
			"nix [path-info --derivation git+file:///repo?rev=base-sha#devShells.x86_64-linux.default]": "/nix/store/222-drv",
			"nix [store diff-closures /nix/store/222-drv /nix/store/111-drv]":                           "bash: 5.2 → 5.3\n",
		},
	}

	engine := &Engine{
		Exec:    mock,
		RepoDir: "/repo",
	}

	results, err := engine.Run(context.Background(), "base-sha", "devShells.${SYSTEM}.default")
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
