# Docker Deployment Guide

YALS Lite provides multiple Docker deployment options to suit different needs.

## Quick Start

### Option 1: Using Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/TogawaSakiko363/YALS_Lite.git
cd YALS_Lite

# Start the service
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the service
docker-compose down
```

Access the application at `http://localhost:8080`

### Option 2: Using Docker CLI

```bash
# Build the image
docker build -t yals-lite .

# Run the container
docker run -d \
  --name yals \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  --cap-add=NET_RAW \
  --cap-add=NET_ADMIN \
  yals-lite

# View logs
docker logs -f yals

# Stop the container
docker stop yals
docker rm yals
```

## Dockerfile Variants

### 1. Dockerfile (Default - Full Featured)

**Size**: ~20MB  
**Features**: All network diagnostic tools included

```bash
docker build -t yals-lite:full -f Dockerfile .
```

**Includes**:
- ping
- traceroute
- mtr
- dig/nslookup
- curl

### 2. Dockerfile.alpine (Minimal)

**Size**: ~15MB  
**Features**: Basic network tools

```bash
docker build -t yals-lite:alpine -f Dockerfile.alpine .
```

**Includes**:
- ping
- dig/nslookup

### 3. Dockerfile.scratch (Ultra-Minimal)

**Size**: ~10MB  
**Features**: Binary only, no system tools

```bash
docker build -t yals-lite:scratch -f Dockerfile.scratch .
```

**Note**: Only works with commands that don't require system tools.

## Configuration

### Using Custom Config

Create your `config.yaml`:

```yaml
listen:
  host: "0.0.0.0"
  port: 8080
  log_level: "info"

rate_limit:
  enabled: true
  max_commands: 10
  time_window: 60

info:
  name: "My Server"
  location: "City, Country"
  datacenter: "DC1"
  test_ip: "192.0.2.1"
  description: "Network diagnostic server"

commands:
  ping:
    template: "ping -c 4"
    description: "ICMP Ping test"
    ignore_target: false
```

Mount it when running:

```bash
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  yals-lite
```

### Environment Variables

```bash
docker run -d \
  -p 8080:8080 \
  -e TZ=America/New_York \
  yals-lite
```

## Docker Compose Configuration

### Basic Setup

```yaml
version: '3.8'

services:
  yals:
    build: .
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
    restart: unless-stopped
```

### With TLS/HTTPS

```yaml
version: '3.8'

services:
  yals:
    build: .
    ports:
      - "443:8080"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./certs/cert.pem:/app/cert.pem:ro
      - ./certs/key.pem:/app/key.pem:ro
    environment:
      - TZ=UTC
    restart: unless-stopped
```

Update `config.yaml`:

```yaml
listen:
  tls: true
  tls_cert_file: "/app/cert.pem"
  tls_key_file: "/app/key.pem"
```

### Behind Reverse Proxy (Nginx)

```yaml
version: '3.8'

services:
  yals:
    build: .
    expose:
      - "8080"
    networks:
      - web
    restart: unless-stopped

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./certs:/etc/nginx/certs:ro
    depends_on:
      - yals
    networks:
      - web
    restart: unless-stopped

networks:
  web:
    driver: bridge
```

## Security Best Practices

### 1. Run as Non-Root User

All provided Dockerfiles create and use a non-root user (`yals:1000`).

### 2. Minimal Capabilities

Only grant necessary Linux capabilities:

```yaml
cap_drop:
  - ALL
cap_add:
  - NET_RAW    # For ping
  - NET_ADMIN  # For traceroute
```

### 3. Read-Only Config

Mount configuration as read-only:

```bash
-v $(pwd)/config.yaml:/app/config.yaml:ro
```

### 4. Resource Limits

Set CPU and memory limits:

```yaml
deploy:
  resources:
    limits:
      cpus: '1'
      memory: 512M
```

### 5. Network Isolation

Use custom networks:

```yaml
networks:
  yals-network:
    driver: bridge
```

## Building for Production

### Multi-Architecture Build

```bash
# Enable buildx
docker buildx create --use

# Build for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  -t yourusername/yals-lite:latest \
  --push \
  .
```

### Optimized Build

```bash
# Build with build cache
docker build \
  --build-arg BUILDKIT_INLINE_CACHE=1 \
  -t yals-lite:latest \
  .

# Build without cache
docker build --no-cache -t yals-lite:latest .
```

## Troubleshooting

### Container Won't Start

Check logs:
```bash
docker logs yals
```

### Permission Issues

Ensure config file is readable:
```bash
chmod 644 config.yaml
```

### Network Tools Not Working

Verify capabilities:
```bash
docker run --rm yals-lite ping -c 1 8.8.8.8
```

If ping fails, add capabilities:
```bash
docker run --cap-add=NET_RAW yals-lite
```

### Port Already in Use

Change the host port:
```bash
docker run -p 8081:8080 yals-lite
```

## Health Checks

The default Dockerfile includes a health check:

```dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/ || exit 1
```

Check container health:
```bash
docker ps
# Look for "healthy" status
```

## Updating

### Pull Latest Changes

```bash
git pull origin main
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Using Pre-built Images

```bash
docker pull yourusername/yals-lite:latest
docker-compose up -d
```

## Performance Tips

1. **Use Alpine-based images** for smaller size
2. **Enable BuildKit** for faster builds
3. **Use multi-stage builds** to reduce final image size
4. **Mount volumes** for config instead of rebuilding
5. **Set resource limits** to prevent resource exhaustion

## Example Production Setup

```yaml
version: '3.8'

services:
  yals:
    image: yals-lite:latest
    container_name: yals-production
    restart: always
    ports:
      - "8080:8080"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
      - ./logs:/app/logs
    environment:
      - TZ=UTC
    deploy:
      resources:
        limits:
          cpus: '2'
          memory: 1G
        reservations:
          cpus: '0.5'
          memory: 256M
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    cap_add:
      - NET_RAW
      - NET_ADMIN
    networks:
      - yals-net
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/"]
      interval: 30s
      timeout: 3s
      retries: 3
      start_period: 10s
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

networks:
  yals-net:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.0.0/16
```

---

For more information, see the main [README.md](README.md).
