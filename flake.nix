{
  description = "SIP call bridging tool for VOIP.MS";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable-small";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages = {
          default = pkgs.buildGoModule {
            pname = "siplink";
            version = "1.0.0";
            
            src = ./.;
            
            vendorHash = "sha256-ZRMpiDSJXzbHWNjMIHkxn0dJjzneudR/SsLl26oETtM=";
            
            # Use proxyVendor due to embedded test files in sipgo dependency
            # The sipgo library has //go:embed directives for test certificates
            # that aren't included in standard vendoring. This is a known issue
            # with Go modules that use embed for test data.
            # See: https://github.com/NixOS/nixpkgs/issues/86349
            proxyVendor = true;
            
            meta = with pkgs.lib; {
              description = "SIP call bridging tool for VOIP.MS";
              homepage = "https://github.com/ak2k/siplink";
              mainProgram = "siplink";
              license = licenses.mit;
              maintainers = [ ];
            };
          };
        };

        # Development shell
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
          ];
          
          shellHook = ''
            echo "🚀 SIPLink Development Environment"
            echo "Go version: $(go version)"
            echo ""
            echo "Available commands:"
            echo "  go run main.go <phone1> <phone2>"
            echo "  go build"
            echo "  go test"
          '';
        };

        # Application runner
        apps.default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/siplink";
        };
      });
}