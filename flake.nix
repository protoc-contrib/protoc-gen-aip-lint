{
  description = "protoc-gen-aip-lint - A protoc plugin for the Google API Linter";

  nixConfig = {
    extra-substituters = [ "https://ralch.cachix.org" ];
    extra-trusted-public-keys = [ "ralch.cachix.org-1:ti5RSqCJwd+VIv7dlxJEAVD98KIJDLxj1yJY+S80Q3A=" ];
  };

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = (pkgs.lib.importJSON ./.github/config/release-please-manifest.json).".";

        package = pkgs.buildGoModule {
          pname = "protoc-gen-aip-lint";
          inherit version;
          src = pkgs.lib.cleanSource ./.;
          subPackages = [ "cmd/protoc-gen-aip-lint" ];
          vendorHash = "sha256-pqrPTxxLfLocWQlDlY30cSqiP+rxuLmfFYw+nlgL+Uk=";
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
        packages.default = package;

        devShells.default = pkgs.mkShell {
          name = "protoc-gen-aip-lint";
          packages = [
            pkgs.go
          ];
        };
      }
    );
}
