package diff

import (
	"strings"
)

// Package represents an updated or newly added package in a closure diff.
type Package struct {
	Name    string
	Version string
}

// ExtractUpdatedPackages parses normalized nix store diff-closures text and
// returns the list of packages that were updated or added to a new version.
//
// Packages with only size changes, deletions (-> ∅), or missing versions (ε) are ignored.
func ExtractUpdatedPackages(diffText string) []Package {
	var pkgs []Package
	seen := make(map[string]bool)

	lines := strings.Split(diffText, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		colonIdx := strings.Index(trimmed, ":")
		if colonIdx == -1 {
			continue
		}

		pkgName := strings.TrimSpace(trimmed[:colonIdx])
		rest := strings.TrimSpace(trimmed[colonIdx+1:])

		// Arrow can be "→" (\u2192) or ASCII "->"
		arrowIdx := strings.Index(rest, "→")
		arrowLen := len("→")
		if arrowIdx == -1 {
			arrowIdx = strings.Index(rest, "->")
			arrowLen = len("->")
		}
		if arrowIdx == -1 {
			// Size-only change line (e.g. "bluez-qt: +12.6 KiB")
			continue
		}

		afterPart := strings.TrimSpace(rest[arrowIdx+arrowLen:])

		// Strip trailing size diff (e.g. ", +13.9 KiB" or ", -5.0 KiB")
		tokens := strings.Split(afterPart, ",")
		var rawVersions []string
		for _, token := range tokens {
			t := strings.TrimSpace(token)
			if t == "" || t == "∅" || t == "ε" {
				continue
			}
			// Size diffs start with '+' or '-' and end with 'B' (e.g. "+13.9 KiB", "-6597.8 KiB")
			if (strings.HasPrefix(t, "+") || strings.HasPrefix(t, "-")) && strings.HasSuffix(t, "B") {
				continue
			}
			rawVersions = append(rawVersions, t)
		}

		if len(rawVersions) == 0 {
			continue
		}
		for _, rawVer := range rawVersions {
			v := strings.TrimSpace(rawVer)
			if v == "" || v == "∅" || v == "ε" {
				continue
			}

			key := pkgName + "@" + v
			if !seen[key] {
				seen[key] = true
				pkgs = append(pkgs, Package{
					Name:    pkgName,
					Version: v,
				})
			}
		}
	}

	return pkgs
}
