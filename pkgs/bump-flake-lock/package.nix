{
  lib,
  buildGo127Module,
  makeWrapper,
  betterleaks,
}:

buildGo127Module (finalAttrs: {
  pname = "bump-flake-lock";
  version = "0.1.0";

  __structuredAttrs = true;

  src = lib.fileset.toSource {
    root = ../../.;
    fileset = lib.fileset.unions [
      ../../go.mod
      ../../internal
      (lib.fileset.fileFilter (file: file.name != "README.md") ./.)
    ];
  };

  vendorHash = null;

  subPackages = [ "pkgs/bump-flake-lock" ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    wrapProgram $out/bin/bump-flake-lock --prefix PATH : ${
      lib.makeBinPath [
        betterleaks
      ]
    }
  '';

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "bump-flake-lock";
    description = "Update flake.lock and create a pull request";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
