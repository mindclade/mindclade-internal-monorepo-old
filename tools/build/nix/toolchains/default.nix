{ pkgs, versions, ... }:
{
  rust = import ./rust.nix { inherit pkgs versions; };
}
