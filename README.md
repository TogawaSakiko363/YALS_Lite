# YALS Lite

**Yet Another Looking Glass** - A lightweight, modern web-based network diagnostic tool.

## Features

- 🚀 **Fast & Lightweight** - Built with Go for optimal performance
- 🎨 **Modern UI** - Clean, responsive interface with real-time output
- 🔒 **Rate Limiting** - Built-in protection against abuse
- 🌐 **IPv4 & IPv6** - Full support for both protocols
- ⚡ **Real-time Streaming** - Live command output via WebSocket
- 🛑 **Command Control** - Stop running commands anytime
- 📱 **Mobile Friendly** - Responsive design for all devices

## Quick Start

#### Installation Prerequisites

- Go 1.25.5 or higher
- Linux/Windows/macOS

#### Installation

1. Clone the repository:
```bash
git clone https://github.com/TogawaSakiko363/YALS_Lite.git
cd YALS_Lite
```

2. Build the application:
```bash
# Linux/macOS
go build -o yals ./cmd/main.go

# Windows
go build -o yals.exe ./cmd/main.go
```

Or use the build script for multiple platforms:
```bash
# Windows
build_binaries.bat
```

## Configuration

Edit `config.yaml` to customize your setup:

```yaml
# Server settings
listen:
  host: "0.0.0.0"
  port: 8080
  log_level: "info"
  tls: false

# Rate limiting
rate_limit:
  enabled: true
  max_commands: 10
  time_window: 60

# Server information
info:
  name: "My Server"
  location: "City, Country"
  datacenter: "DC1"
  test_ip: "192.0.2.1"
  description: "Network diagnostic server"

# Available commands
commands:
  ping:
    template: "ping -c 4"
    ignore_target: false
  traceroute:
    template: "traceroute"
    ignore_target: false
  uname:
    template: "uname -a"
    ignore_target: true
```

### Command Configuration

- **template**: The command to execute
- **description**: User-friendly description
- **ignore_target**: Set to `true` if command doesn't need a target (e.g., system info)

## Supported Input Formats

YALS accepts various target formats:

- **IPv4**: `192.0.2.1`, `192.0.2.1:80`
- **IPv6**: `2001:db8::1`, `[2001:db8::1]:80`
- **Domain**: `example.com`, `example.com:443`

## Security

### Rate Limiting

Configure rate limiting to prevent abuse:

```yaml
rate_limit:
  enabled: true
  max_commands: 10    # Maximum commands per time window
  time_window: 60     # Time window in seconds
```

### TLS Support

Enable HTTPS for secure connections:

```yaml
listen:
  tls: true
  tls_cert_file: "./cert.pem"
  tls_key_file: "./key.pem"
```

## Building for Multiple Platforms

The included build script creates binaries for:

- Windows (x64, ARM64)
- Linux (x64, ARM64)
- macOS (x64, ARM64/Apple Silicon)

```bash
# Windows
build_binaries.bat

# Output in bin/ directory
bin/
├── yals_windows_amd64.exe
├── yals_windows_arm64.exe
├── yals_linux_amd64
├── yals_linux_arm64
├── yals_darwin_amd64
└── yals_darwin_arm64
```

## Run directly
```bash
./yals -c config.yaml -w ./web
```

## Development

### Project Structure

```
YALS_Lite/
├── cmd/
│   └── main.go           # Application entry point
├── internal/
│   ├── config/           # Configuration management
│   ├── executor/         # Command execution
│   ├── handler/          # HTTP/WebSocket handlers
│   ├── logger/           # Logging utilities
│   ├── utils/            # Helper functions
│   └── validator/        # Input validation
├── web/
│   ├── index.html        # Frontend HTML
│   ├── app.js            # Frontend JavaScript
│   └── style.css         # Frontend styles
├── config.yaml           # Configuration file
└── README.md
```

### Running in Development

```bash
go run ./cmd/main.go
```

## License

This project is licensed under the GNU Affero General Public License v3.0 License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For issues and questions, please open an issue on GitHub.

---

**Made with ❤️ by the YALS Team**
