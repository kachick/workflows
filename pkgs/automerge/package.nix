{
  lib,
  buildGo127Module,
  makeWrapper,
  osv-scanner,
  git,
  gnutar,
}:

buildGo127Module (finalAttrs: {
  pname = "automerge";
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

  checkFlags = [
    "-skip=^TestReasonDocs"
  ];

  vendorHash = null;

  subPackages = [ "pkgs/automerge" ];

  nativeBuildInputs = [ makeWrapper ];

  postInstall = ''
    wrapProgram $out/bin/automerge --prefix PATH : ${
      lib.makeBinPath [
        osv-scanner
        git
        gnutar
      ]
    }
  '';

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "automerge";
    description = "Merge eligible bot pull requests";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
