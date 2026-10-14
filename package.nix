{
  lib,
  buildGo127Module,
}:

buildGo127Module (finalAttrs: {
  pname = "nix-diff-closures-action";
  version = "0.1.0";

  __structuredAttrs = true;

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./go.mod
      ./main.go
      ./internal
    ];
  };

  vendorHash = null;

  env.CGO_ENABLED = "0";

  meta = {
    mainProgram = "nix-diff-closures-action";
    description = "GitHub Action to diff closures of Nix derivations";
    homepage = "https://github.com/kachick/nix-diff-closures-action";
    license = lib.licenses.mit;
  };
})
