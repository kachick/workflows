{
  lib,
  buildGo127Module,
}:

buildGo127Module (finalAttrs: {
  pname = "automerge";
  version = "0.1.0";

  __structuredAttrs = true;

  src = lib.fileset.toSource {
    root = ../../.;
    fileset = lib.fileset.unions [
      ../../go.mod
      ./.
    ];
  };

  vendorHash = null;

  env.CGO_ENABLED = "0";

  passthru.shared-gomod = true;

  meta = {
    mainProgram = "automerge";
    description = "Merge eligible bot pull requests";
    homepage = "https://github.com/kachick/workflows";
    license = lib.licenses.mit;
  };
})
