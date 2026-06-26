---
icon: material/docker
---

# Docker

## :material-console: 命令

```bash
docker run -d \
  -v /etc/singlink:/etc/singlink/ \
  --name=singlink \
  --restart=always \
  ghcr.io/sagernet/singlink \
  -D /var/lib/singlink \
  -C /etc/singlink/ run
```

## :material-box-shadow: Compose

```yaml
version: "3.8"
services:
  singlink:
    image: ghcr.io/sagernet/singlink
    container_name: singlink
    restart: always
    volumes:
      - /etc/singlink:/etc/singlink/
    command: -D /var/lib/singlink -C /etc/singlink/ run
```
