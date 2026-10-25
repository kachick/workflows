package diff

import (
	"reflect"
	"testing"
)

func TestExtractUpdatedPackages(t *testing.T) {
	tests := []struct {
		name     string
		diffText string
		expected []Package
	}{
		{
			name:     "empty input returns empty slice",
			diffText: "",
			expected: nil,
		},
		{
			name: "parses version bump and additions while ignoring deletions and size-only lines",
			diffText: `
acpi-call: 2020-04-07-5.8.16 → 2020-04-07-5.8.18
baloo-widgets: 20.08.1 → 20.08.2
bluez-qt: +12.6 KiB
dolphin: 20.08.1 → 20.08.2, +13.9 KiB
kdeconnect: 20.08.2 → ∅, -6597.8 KiB
kdeconnect-kde: ∅ → 20.08.2, +6599.7 KiB
n4hz2f0rhvclfbgk4adkbxi6y8sd31cf: ε → ∅, -199.8 MiB
rfcpg2ryywgma7wlswjzna99g9gq8laq: ∅ → ε, +199.8 MiB
zod: 3.22.0 -> 3.22.4, +5.0 KiB
`,
			expected: []Package{
				{Name: "acpi-call", Version: "2020-04-07-5.8.18"},
				{Name: "baloo-widgets", Version: "20.08.2"},
				{Name: "dolphin", Version: "20.08.2"},
				{Name: "kdeconnect-kde", Version: "20.08.2"},
				{Name: "zod", Version: "3.22.4"},
			},
		},
		{
			name: "deduplicates identical packages",
			diffText: `
zod: 3.22.0 → 3.22.4
zod: 3.22.0 → 3.22.4
`,
			expected: []Package{
				{Name: "zod", Version: "3.22.4"},
			},
		},
		{
			name: "handles multiple target versions on same line",
			diffText: `
libfoo: 1.2 → 1.3, 1.4
`,
			expected: []Package{
				{Name: "libfoo", Version: "1.3"},
				{Name: "libfoo", Version: "1.4"},
			},
		},
		{
			name: "ignores patch file derivation tokens in closure diffs",
			diffText: `
chromium: 141-rust.patch, 148-revert.patch.drv → ∅, -17.3 KiB
archlinux-chromium: 146-drop-unknown-clang-flag.patch.drv → ∅
`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractUpdatedPackages(tt.diffText)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("expected %#v, got %#v", tt.expected, got)
			}
		})
	}
}

func TestExtractPackageChanges(t *testing.T) {
	diffText := `
acpi-call: 2020-04-07-5.8.16 → 2020-04-07-5.8.18
bluez-qt: +12.6 KiB
kdeconnect: 20.08.2 → ∅, -6597.8 KiB
kdeconnect-kde: ∅ → 20.08.2, +6599.7 KiB
nix: 2.31.5.drv, 2.34.8.drv → 2.31.6.drv, 2.34.9.drv
rfcpg2ryywgma7wlswjzna99g9gq8laq: ε → ∅, -199.8 MiB
zod: 3.22.0 -> 3.22.4, +5.0 KiB
`
	expectedBefore := []Package{
		{Name: "acpi-call", Version: "2020-04-07-5.8.16"},
		{Name: "kdeconnect", Version: "20.08.2"},
		{Name: "nix", Version: "2.31.5.drv"},
		{Name: "nix", Version: "2.34.8.drv"},
		{Name: "zod", Version: "3.22.0"},
	}
	expectedAfter := []Package{
		{Name: "acpi-call", Version: "2020-04-07-5.8.18"},
		{Name: "kdeconnect-kde", Version: "20.08.2"},
		{Name: "nix", Version: "2.31.6.drv"},
		{Name: "nix", Version: "2.34.9.drv"},
		{Name: "zod", Version: "3.22.4"},
	}

	before, after := ExtractPackageChanges(diffText)
	if !reflect.DeepEqual(before, expectedBefore) {
		t.Fatalf("before mismatch:\nexpected: %#v\ngot:      %#v", expectedBefore, before)
	}
	if !reflect.DeepEqual(after, expectedAfter) {
		t.Fatalf("after mismatch:\nexpected: %#v\ngot:      %#v", expectedAfter, after)
	}
}
