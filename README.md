# SIPLink

A lightweight SIP call bridging tool for VOIP.MS that connects two phone numbers via automated transfer.

## Features

- 🔗 Bridges two phone calls using SIP REFER method
- 📞 HD voice support with G.722 codec
- 🚀 Fast and lightweight Go implementation
- 🔒 Secure digest authentication
- 📦 Nix flake for reproducible builds
- 🖥️ Cross-platform support (macOS, Linux)

## Installation

### Using Nix (Recommended)

```bash
# Run directly
nix run github:ak2k/siplink -- 15551234567 15559876543

# Install to profile
nix profile install github:ak2k/siplink
```

### From Source

```bash
git clone https://github.com/ak2k/siplink
cd siplink
go build -o siplink main.go
```

## Configuration

Set your VOIP.MS credentials as environment variables:

```bash
export VOIPMS_USER='your_voipms_username'
export VOIPMS_PASS='your_voipms_password'
export VOIPMS_SERVER='chicago.voip.ms'  # Optional, defaults to chicago
```

### Available VOIP.MS Servers

- `chicago.voip.ms`, `newyork.voip.ms`, `seattle.voip.ms`
- `dallas.voip.ms`, `losangeles.voip.ms`
- `toronto.voip.ms`, `montreal.voip.ms`

## Usage

```bash
# Basic usage
siplink <source_number> <destination_number>

# Example
siplink 15551234567 15559876543

# With environment override
VOIPMS_SERVER=toronto.voip.ms siplink 14161234567 14169876543
```

## Integration with Password Managers

### Bitwarden

Using [rbw](https://github.com/doy/rbw), a fast Bitwarden CLI:

1. **Store your credentials in Bitwarden:**
   - Item name: `voipms` (or any name you prefer)
   - Custom fields:
     - `voipms_user`: Your VOIP.MS username
     - `voipms_pass`: Your VOIP.MS password
     - `voipms_server`: Your preferred server (e.g., `chicago.voip.ms`)

2. **Install and configure rbw:**
   ```bash
   # Install rbw
   nix-env -iA nixpkgs.rbw  # or add to your nix configuration

   # First-time setup
   rbw config set email your.email@example.com
   rbw login
   rbw sync
   ```

3. **Use with siplink:**
   ```bash
   # Set credentials from Bitwarden
   export VOIPMS_USER=$(rbw get "voipms" --field voipms_user)
   export VOIPMS_PASS=$(rbw get "voipms" --field voipms_pass)
   export VOIPMS_SERVER=$(rbw get "voipms" --field voipms_server)

   # Run siplink
   siplink 15551234567 15559876543
   ```

### Shell Aliases

Add these to your shell configuration for convenience:

```bash
# Basic call - expects VOIPMS_* env vars
alias call="nix run github:ak2k/siplink --"

# Quick transfer to your frequent number
alias callt="nix run github:ak2k/siplink -- 15551234567"

# With rbw (Bitwarden) - fetches credentials on each run
alias callb='export VOIPMS_USER=$(rbw get "voipms" --field voipms_user) && \
             export VOIPMS_PASS=$(rbw get "voipms" --field voipms_pass) && \
             export VOIPMS_SERVER=$(rbw get "voipms" --field voipms_server) && \
             nix run github:ak2k/siplink --'
```

## How It Works

1. **Registration**: Authenticates with VOIP.MS SIP server
2. **Call Initiation**: Places call to first number
3. **Transfer**: Uses SIP REFER to transfer call to second number
4. **Monitoring**: Tracks transfer progress via NOTIFY messages
5. **Completion**: Exits cleanly when transfer succeeds

## Requirements

- VOIP.MS account with sub-account credentials
- Go 1.21+ (for building from source)
- Nix (for nix-based installation)

## Troubleshooting

### Number Format
Use 11-digit format for US/Canada numbers: `1XXXXXXXXXX`

### Authentication Failures
- Verify credentials are correct
- Check if your IP is whitelisted in VOIP.MS settings
- Try a different server if connection fails

### Transfer Issues
Some carriers may not support SIP REFER transfers. Test with known working numbers first.

## License

MIT License - see LICENSE file for details

## Contributing

Pull requests welcome! Please ensure:
- Code follows Go conventions
- Tests pass
- Documentation is updated

## Acknowledgments

Built with [SIPGO](https://github.com/emiago/sipgo) - a modern SIP library for Go.