{
  lib,
  buildGo127Module,
  makeBinaryWrapper,
  betterleaks,
}:

buildGo127Module (finalAttrs: {
  pname = "cleanup-ghcr";
  version = "0.1.0";

  __structuredAttrs = true;

  src = lib.fileset.toSource {
    root = ../../.;
    fileset = lib.fileset.unions [
      ../../go.mod
      ../../internal
      (lib.fileset.fileFilter (file: !file.hasExt "md") ./.)
    ];
  };

  vendorHash = null;

  subPackages = [ "pkgs/cleanup-ghcr" ];

  nativeBuildInputs = [ makeBinaryWrapper ];

  postInstall = ''
    wrapProgram $out/bin/cleanup-ghcr --prefix PATH : ${
      lib.makeBinPath [
        betterleaks
      ]
    }
  '';

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "cleanup-ghcr";
    description = "Prune old or PR staging GHCR container package versions";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
