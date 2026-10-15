{
  lib,
  buildGo127Module,
  makeWrapper,
  betterleaks,
}:

buildGo127Module (finalAttrs: {
  pname = "nix-diff-closures";
  version = "0.1.0";

  __structuredAttrs = true;

  src = lib.fileset.toSource {
    root = ../../.;
    fileset = lib.fileset.unions [
      ../../go.mod
      ../../internal
      ./.
    ];
  };

  vendorHash = null;

  subPackages = [ "pkgs/nix-diff-closures" ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    wrapProgram $out/bin/nix-diff-closures --prefix PATH : ${
      lib.makeBinPath [
        betterleaks
      ]
    }
  '';

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "nix-diff-closures";
    description = "CLI tool to diff closures of Nix derivations";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
