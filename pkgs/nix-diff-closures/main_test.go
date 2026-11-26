package main

import (
	"testing"

	"github.com/kachick/workflows/pkgs/nix-diff-closures/internal/osv"
)

func TestParseConfigPrecedenceAndValidation(t *testing.T) {
	t.Run("explicit CLI flags take precedence over environment variables", func(t *testing.T) {
		t.Setenv("BASE_SHA", "env-sha")
		t.Setenv("TARGETS", "env-target")
		t.Setenv("CHECK_VULNERABILITIES", "false")
		t.Setenv("OSV_ENDPOINT", "https://env.example/osv")
		t.Setenv("FAIL_ON_VULNERABILITY", "true")

		cfg, err := parseConfig([]string{
			"--base-rev=cli-sha",
			"--targets=cli-target",
			"--check-vulnerabilities=true",
			"--osv-endpoint=https://cli.example/osv",
			"--fail-on-vulnerability=false",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.baseRev != "cli-sha" || cfg.targets != "cli-target" {
			t.Errorf("unexpected baseRev/targets: %q / %q", cfg.baseRev, cfg.targets)
		}
		if !cfg.checkVulns {
			t.Error("expected checkVulns=true from CLI flag")
		}
		if cfg.osvEndpoint != "https://cli.example/osv" {
			t.Errorf("osvEndpoint = %q, want CLI value", cfg.osvEndpoint)
		}
		if cfg.failOnVuln {
			t.Error("expected failOnVuln=false from CLI flag")
		}
	})

	t.Run("rejects invalid boolean env var", func(t *testing.T) {
		t.Setenv("BASE_SHA", "abc")
		t.Setenv("TARGETS", "devShells.x86_64-linux.default")
		t.Setenv("CHECK_VULNERABILITIES", "not-a-bool")

		if _, err := parseConfig(nil); err == nil {
			t.Fatal("expected error for invalid CHECK_VULNERABILITIES, got nil")
		}
	})
}

func TestClassifyVulnerabilities(t *testing.T) {
	before := []osv.Package{
		{Name: "openssl", Version: "3.0.0"},
		{Name: "curl", Version: "8.0.0"},
	}
	after := []osv.Package{
		{Name: "openssl", Version: "3.0.1"},
		{Name: "curl", Version: "8.1.0"},
	}
	batch := []osv.PackageVulnerabilities{
		{
			Package:         osv.Package{Name: "openssl", Version: "3.0.0"},
			Vulnerabilities: []osv.Vulnerability{{ID: "CVE-2026-0001"}, {ID: "CVE-2026-0002"}},
		},
		{
			Package:         osv.Package{Name: "openssl", Version: "3.0.1"},
			Vulnerabilities: []osv.Vulnerability{{ID: "CVE-2026-0002"}, {ID: "CVE-2026-0003"}},
		},
		{
			Package:         osv.Package{Name: "curl", Version: "8.0.0"},
			Vulnerabilities: []osv.Vulnerability{{ID: "CVE-2026-1000"}},
		},
	}

	got := classifyVulnerabilities(before, after, batch)

	if len(got.NewVulns) != 1 || got.NewVulns[0].Package.Name != "openssl" || len(got.NewVulns[0].Vulnerabilities) != 1 || got.NewVulns[0].Vulnerabilities[0].ID != "CVE-2026-0003" {
		t.Errorf("unexpected NewVulns: %+v", got.NewVulns)
	}
	if len(got.ExistingVulns) != 1 || got.ExistingVulns[0].Package.Name != "openssl" || len(got.ExistingVulns[0].Vulnerabilities) != 1 || got.ExistingVulns[0].Vulnerabilities[0].ID != "CVE-2026-0002" {
		t.Errorf("unexpected ExistingVulns: %+v", got.ExistingVulns)
	}
	if len(got.ResolvedVulns) != 2 {
		t.Errorf("expected 2 resolved package entries (openssl + curl), got %+v", got.ResolvedVulns)
	}
}
