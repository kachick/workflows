{
  lib,
  buildGo127Module,
  makeBinaryWrapper,
  cachix,
}:

buildGo127Module (finalAttrs: {
  pname = "cachixpin";
  version = "0.1.0";

  __structuredAttrs = true;

  src = lib.fileset.toSource {
    root = ../../.;
    fileset = lib.fileset.unions [
      ../../go.mod
      (lib.fileset.fileFilter (file: !file.hasExt "md") ./.)
    ];
  };

  vendorHash = null;

  subPackages = [ "pkgs/cachixpin" ];

  nativeBuildInputs = [ makeBinaryWrapper ];

  postInstall = ''
    wrapProgram $out/bin/cachixpin --prefix PATH : ${
      lib.makeBinPath [
        cachix
      ]
    }
  '';

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "cachixpin";
    description = "Push and pin flake packages on Cachix";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
