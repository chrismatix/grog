# Taken from here
# https://discourse.nixos.org/t/very-basic-how-do-i-build-a-go-package/59830/2
let
  pkgs = import <nixpkgs> { };
in
pkgs.callPackage ./package.nix { }
