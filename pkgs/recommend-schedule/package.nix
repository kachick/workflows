{
  lib,
  buildGo127Module,
}:

buildGo127Module (finalAttrs: {
  pname = "recommend-schedule";
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

  subPackages = [ "pkgs/recommend-schedule" ];

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "recommend-schedule";
    description = "Generate deterministic cron schedules for workflows";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
