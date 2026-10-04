{
  description = "Grog, a lightweight mono-repo build orchestrator";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          versionInfo = {
            version = self.shortRev or self.dirtyShortRev or "dev";
            commit = self.rev or self.dirtyRev or "unknown";
          };
          grog = pkgs.callPackage ./package.nix versionInfo;
          grog-full = pkgs.callPackage ./package.nix (versionInfo // { withDuckdb = true; });
        in
        {
          inherit grog grog-full;
          default = grog;
          grog-with-pkl = pkgs.symlinkJoin {
            name = "grog-with-pkl";
            paths = [ grog pkgs.pkl ];
          };
        }
      );

      overlays.default = final: prev: {
        grog = final.callPackage ./package.nix { };
        grog-full = final.callPackage ./package.nix { withDuckdb = true; };
      };
    };
}
