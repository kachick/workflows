{
  writeShellApplication,
  git,
  gh,
  nix,
}:

writeShellApplication {
  name = "bump-flake-lock";
  runtimeInputs = [
    git
    gh
    nix
  ];
  text = builtins.readFile ./bump-flake-lock.bash;
}
