{
  inputs = {
    nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst";
  };

  outputs =
    {
      nixpkgs,
      ...
    }:
    let
      inherit (nixpkgs) lib;
      forAllSystems = lib.genAttrs lib.systems.flakeExposed;
    in
    {
      formatter = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        pkgs.writeShellScriptBin "dprint-fmt" ''
          exec "${lib.getExe pkgs.dprint}" fmt "$@"
        ''
      );

      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          nix-diff-closures = pkgs.callPackage ./pkgs/nix-diff-closures/package.nix { };
          bump-flake-lock = pkgs.callPackage ./pkgs/bump-flake-lock/package.nix { };
          automerge = pkgs.callPackage ./pkgs/automerge/package.nix { };
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShellNoCC {
            env = {
              # Set NIX_PATH for nixd inlay hints
              NIX_PATH = "nixpkgs=${nixpkgs.outPath}";

              # For local development
              CGO_ENABLED = "0";
            };

            buildInputs = (
              with pkgs;
              [
                # https://github.com/NixOS/nix/issues/730#issuecomment-162323824
                bashInteractive
                findutils # xargs
                nixd
                go-task

                go_1_27
                betterleaks

                dprint
                typos
                shellcheck
                zizmor
              ]
            );
          };
        }
      );
    };
}
