{
  description = "Sneakernet: offline Xray installer and manager for a Ventoy USB stick";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;
      # The binary targets Linux (systemd, /etc, /opt). The dev shell also
      # works on macOS, since `make build` cross-compiles for Linux anyway.
      linuxSystems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      allSystems = linuxSystems ++ [
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forSystems = systems: f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = self.shortRev or self.dirtyShortRev or "dev";
    in
    {
      packages = forSystems linuxSystems (pkgs: {
        sneakernet = pkgs.buildGoModule {
          pname = "sneakernet";
          inherit version;

          # Only the Go sources: editing docs or scripts does not rebuild.
          src = lib.fileset.toSource {
            root = ./.;
            fileset = lib.fileset.unions [
              ./go.mod
              ./go.sum
              ./cmd
              ./internal
              ./test/fixtures
            ];
          };

          # Hash of `go mod vendor` for go.mod/go.sum. After changing
          # dependencies, set it to lib.fakeHash, run `nix build` and copy
          # the hash from the error.
          vendorHash = "sha256-jyziX7z11hd8rgqBFQqGorxxSbT010XApmo47QUZuf8=";

          subPackages = [ "cmd/sneakernet" ];
          env.CGO_ENABLED = "0";
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          meta = {
            description = "Offline Xray client installer and manager (CLI + TUI)";
            homepage = "https://github.com/AliSohani2082/sneakernet";
            mainProgram = "sneakernet";
            platforms = lib.platforms.linux;
          };
        };
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.sneakernet;
      });

      apps = forSystems linuxSystems (pkgs: {
        default = {
          type = "app";
          program = lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.sneakernet;
          meta.description = "Run the sneakernet CLI";
        };
      });

      # `nix flake check` builds the package, which runs the cmd/sneakernet tests.
      checks = forSystems linuxSystems (pkgs: {
        inherit (self.packages.${pkgs.stdenv.hostPlatform.system}) sneakernet;
      });

      # Everything the Makefile and scripts/ call: make fetch / bundle / lint.
      devShells = forSystems allSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gnumake
            git
            curl
            unzip
            gnutar
            gzip
            coreutils
            shellcheck
          ];
        };
      });

      formatter = forSystems allSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
