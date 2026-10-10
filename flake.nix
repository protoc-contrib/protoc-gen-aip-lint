{
  description = "protoc-gen-aip-lint - A protoc plugin for the Google API Linter";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    nix-release-bin = {
      url = "github:nixos-contrib/nix-release-bin";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      nix-release-bin,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = (pkgs.lib.importJSON ./.github/config/release-please-manifest.json).".";

        source = pkgs.buildGoModule {
          pname = "protoc-gen-aip-lint";
          inherit version;
          src = pkgs.lib.cleanSource ./.;
          subPackages = [ "cmd/protoc-gen-aip-lint" ];
          vendorHash = "sha256-Hr7MRdd4iyY3uLE21sTGKdx92Tkomm5EJD/eUJl1ZX0=";
          # The standard library only uses cgo for net and os/user, which fall
          # back to pure Go without it. The Linux binary is then static, so the
          # release asset runs on any distribution, not only under Nix.
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          meta = with pkgs.lib; {
            description = "A protoc plugin for the Google API Linter";
            license = licenses.mit;
            mainProgram = "protoc-gen-aip-lint";
          };
        };
      in
      {
        packages = {
          # The latest release binary, where it has one for the system: CI pins
          # them in the manifest once the release has published them.
          default = nix-release-bin.lib.mkReleaseBin {
            inherit pkgs;
            manifest = ./.github/config/nix-release-bin-manifest.json;
            pname = "protoc-gen-aip-lint";
            fallback = source;
          };
          inherit source;
        };

        devShells.default = pkgs.mkShell {
          name = "protoc-gen-aip-lint";
          packages = [
            pkgs.go
          ];
        };
      }
    );
}
