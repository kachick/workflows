{
  lib,
  buildGo127Module,
}:

buildGo127Module (finalAttrs: {
  pname = "minimize-pr-comments";
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

  subPackages = [ "pkgs/minimize-pr-comments" ];

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "minimize-pr-comments";
    description = "CLI tool to minimize outdated bot comments on pull requests";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
