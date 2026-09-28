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
          grog = pkgs.callPackage ./package.nix {
            version = self.shortRev or self.dirtyShortRev or "dev";
            commit = self.rev or self.dirtyRev or "unknown";
          };
        in
        {
          inherit grog;
          default = grog;
          grog-with-pkl = pkgs.symlinkJoin {
            name = "grog-with-pkl";
            paths = [ grog pkgs.pkl ];
          };
        }
      );

      overlays.default = final: prev: {
        grog = final.callPackage ./package.nix { };
      };
    };
}
