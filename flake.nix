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
            
            vendorHash = "sha256-WRONGHASH1234567890WRONGHASH1234567890ABC=";
            
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