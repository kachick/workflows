package main

import (
	"testing"
)

func TestDiffFlakeLocks(t *testing.T) {
	t.Run("github, git+github, and nixpkgs channel tarballs", func(t *testing.T) {
		oldJSON := `{
			"nodes": {
				"root": {
					"inputs": {
						"nixpkgs": "nixpkgs",
						"nixos-stable": "nixos-stable",
						"flake-utils": "flake-utils",
						"git-input": "git-input",
						"unchanged": "unchanged"
					}
				},
				"nixpkgs": {
					"locked": {
						"lastModified": 1700000000,
						"narHash": "sha256-old",
						"rev": "1111111111111111111111111111111111111111",
						"type": "tarball",
						"url": "https://releases.nixos.org/nixpkgs/nixpkgs-26.11pre1.111111111111/nixexprs.tar.zst"
					},
					"original": {
						"type": "tarball",
						"url": "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst"
					}
				},
				"nixos-stable": {
					"locked": {
						"rev": "2222222222222222222222222222222222222222",
						"type": "tarball",
						"url": "https://releases.nixos.org/nixos/25.11/nixos-25.11.1.2222222/nixexprs.tar.xz"
					}
				},
				"flake-utils": {
					"locked": {
						"owner": "numtide",
						"repo": "flake-utils",
						"rev": "3333333333333333333333333333333333333333",
						"type": "github"
					}
				},
				"git-input": {
					"locked": {
						"rev": "4444444444444444444444444444444444444444",
						"type": "git",
						"url": "https://github.com/kachick/example.git"
					}
				},
				"unchanged": {
					"locked": {
						"owner": "NixOS",
						"repo": "nixpkgs",
						"rev": "5555555555555555555555555555555555555555",
						"type": "github"
					}
				}
			},
			"root": "root",
			"version": 7
		}`

		newJSON := `{
			"nodes": {
				"root": {
					"inputs": {
						"nixpkgs": "nixpkgs",
						"nixos-stable": "nixos-stable",
						"flake-utils": "flake-utils",
						"git-input": "git-input",
						"unchanged": "unchanged"
					}
				},
				"nixpkgs": {
					"locked": {
						"lastModified": 1700000100,
						"narHash": "sha256-new",
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "tarball",
						"url": "https://releases.nixos.org/nixpkgs/nixpkgs-26.11pre2.aaaaaaaaaaaa/nixexprs.tar.zst"
					},
					"original": {
						"type": "tarball",
						"url": "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst"
					}
				},
				"nixos-stable": {
					"locked": {
						"rev": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
						"type": "tarball",
						"url": "https://releases.nixos.org/nixos/25.11/nixos-25.11.2.bbbbbbb/nixexprs.tar.xz"
					}
				},
				"flake-utils": {
					"locked": {
						"owner": "numtide",
						"repo": "flake-utils",
						"rev": "cccccccccccccccccccccccccccccccccccccccc",
						"type": "github"
					}
				},
				"git-input": {
					"locked": {
						"rev": "dddddddddddddddddddddddddddddddddddddddd",
						"type": "git",
						"url": "https://github.com/kachick/example.git"
					}
				},
				"unchanged": {
					"locked": {
						"owner": "NixOS",
						"repo": "nixpkgs",
						"rev": "5555555555555555555555555555555555555555",
						"type": "github"
					}
				}
			},
			"root": "root",
			"version": 7
		}`

		got, err := diffFlakeLocks([]byte(oldJSON), []byte(newJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []flakeInputUpdate{
			{
				Node:   "flake-utils",
				Owner:  "numtide",
				Repo:   "flake-utils",
				OldRev: "3333333333333333333333333333333333333333",
				NewRev: "cccccccccccccccccccccccccccccccccccccccc",
			},
			{
				Node:   "git-input",
				Owner:  "kachick",
				Repo:   "example",
				OldRev: "4444444444444444444444444444444444444444",
				NewRev: "dddddddddddddddddddddddddddddddddddddddd",
			},
			{
				Node:   "nixos-stable",
				Owner:  "NixOS",
				Repo:   "nixpkgs",
				OldRev: "2222222222222222222222222222222222222222",
				NewRev: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
			{
				Node:   "nixpkgs",
				Owner:  "NixOS",
				Repo:   "nixpkgs",
				OldRev: "1111111111111111111111111111111111111111",
				NewRev: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		}

		if len(got) != len(want) {
			t.Fatalf("got %d updates, want %d: %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("update[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("omits non-github forges, non-nixpkgs tarballs, repo changes, added/removed, and invalid refs", func(t *testing.T) {
		oldJSON := `{
			"nodes": {
				"gitlab-input": {
					"locked": {
						"owner": "org",
						"repo": "proj",
						"rev": "1111111111111111111111111111111111111111",
						"type": "gitlab"
					}
				},
				"ghe-input": {
					"locked": {
						"host": "github.example.com",
						"owner": "org",
						"repo": "proj",
						"rev": "1111111111111111111111111111111111111111",
						"type": "github"
					}
				},
				"other-tarball": {
					"locked": {
						"rev": "1111111111111111111111111111111111111111",
						"type": "tarball",
						"url": "https://releases.nixos.org/nix/nix-2.24.0/nix.tar.xz"
					}
				},
				"channel-no-rev": {
					"locked": {
						"type": "tarball",
						"url": "https://releases.nixos.org/nixpkgs/nixpkgs-26.11pre1/nixexprs.tar.zst"
					}
				},
				"repo-changed": {
					"locked": {
						"owner": "old-org",
						"repo": "proj",
						"rev": "1111111111111111111111111111111111111111",
						"type": "github"
					}
				},
				"removed-input": {
					"locked": {
						"owner": "org",
						"repo": "removed",
						"rev": "1111111111111111111111111111111111111111",
						"type": "github"
					}
				},
				"invalid-owner": {
					"locked": {
						"owner": "org/../../evil",
						"repo": "proj",
						"rev": "1111111111111111111111111111111111111111",
						"type": "github"
					}
				}
			}
		}`

		newJSON := `{
			"nodes": {
				"gitlab-input": {
					"locked": {
						"owner": "org",
						"repo": "proj",
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "gitlab"
					}
				},
				"ghe-input": {
					"locked": {
						"host": "github.example.com",
						"owner": "org",
						"repo": "proj",
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "github"
					}
				},
				"other-tarball": {
					"locked": {
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "tarball",
						"url": "https://releases.nixos.org/nix/nix-2.25.0/nix.tar.xz"
					}
				},
				"channel-no-rev": {
					"locked": {
						"type": "tarball",
						"url": "https://releases.nixos.org/nixpkgs/nixpkgs-26.11pre2/nixexprs.tar.zst"
					}
				},
				"repo-changed": {
					"locked": {
						"owner": "new-org",
						"repo": "proj",
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "github"
					}
				},
				"added-input": {
					"locked": {
						"owner": "org",
						"repo": "added",
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "github"
					}
				},
				"invalid-owner": {
					"locked": {
						"owner": "org/../../evil",
						"repo": "proj",
						"rev": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"type": "github"
					}
				}
			}
		}`

		got, err := diffFlakeLocks([]byte(oldJSON), []byte(newJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected no updates, got %+v", got)
		}
	})

	t.Run("returns error on malformed json", func(t *testing.T) {
		if _, err := diffFlakeLocks([]byte("{"), []byte("{}")); err == nil {
			t.Error("expected error for invalid old json, got nil")
		}
		if _, err := diffFlakeLocks([]byte("{}"), []byte("{")); err == nil {
			t.Error("expected error for invalid new json, got nil")
		}
	})
}

func TestFormatFlakeLockUpdates(t *testing.T) {
	if got := formatFlakeLockUpdates(nil); got != "" {
		t.Errorf("expected empty string for nil updates, got %q", got)
	}

	updates := []flakeInputUpdate{
		{
			Node:   "nixpkgs",
			Owner:  "NixOS",
			Repo:   "nixpkgs",
			OldRev: "6dadaf4375be35033e1c84395adbd53ff5fded96",
			NewRev: "abcdef1234567890abcdef1234567890abcdef12",
		},
	}

	got := formatFlakeLockUpdates(updates)
	want := "### Updated `flake.lock` inputs\n\n" +
		"- `nixpkgs` (`NixOS/nixpkgs`): [`6dadaf4...abcdef1`](https://github.com/NixOS/nixpkgs/compare/6dadaf4375be35033e1c84395adbd53ff5fded96...abcdef1234567890abcdef1234567890abcdef12)"

	if got != want {
		t.Errorf("formatFlakeLockUpdates() =\n%s\nwant:\n%s", got, want)
	}
}
