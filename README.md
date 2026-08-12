# ASDL Hub

> A self-hosted infrastructure hub for managing distributed nodes and services over a WireGuard mesh.

ASDL Hub gives you a central place to manage your infrastructure across multiple machines.

Connect your nodes, manage services and deployments, monitor health, run jobs, and access node terminals from one dashboard.

## Features

- 🖥️ **Node management**
- 🔐 **WireGuard-based node connectivity**
- 📦 **Service and container management**
- 🚀 **Deployments**
- ❤️ **Node health monitoring**
- 🖥️ **Web-based terminal access**
- 🔄 **Migrations between nodes**
- ⚙️ **Job management**
- 🔑 **Built-in node enrollment**
- 🌐 **Web dashboard**

## Architecture

```text
                    ┌─────────────────────┐
                    │      ASDL Hub       │
                    │                     │
                    │  API + Dashboard    │
                    │  PostgreSQL         │
                    │  WireGuard          │
                    └──────────┬──────────┘
                               │
                    WireGuard mesh
              ┌────────────────┼────────────────┐
              │                │                │
        ┌─────▼─────┐    ┌─────▼─────┐    ┌─────▼─────┐
        │   Node    │    │   Node    │    │   Node    │
        │   Agent   │    │   Agent   │    │   Agent   │
        └───────────┘    └───────────┘    └───────────┘
```

The Hub manages the infrastructure while the Agent runs on managed nodes.
Installation
ASDL Hub is designed to be installed with a single command:
```bash
curl -fsSL https://get.asdl.website/asdl-hub | sudo bash
```
The installer handles dependencies, PostgreSQL, WireGuard, Nginx, the firewall, systemd, and the Hub itself.
A domain is optional. If you don't provide one, ASDL Hub can use the server's public IP.

## Documentation
Full documentation:
https://docs.asdl.website/asdl-hub

## Installation:
https://get.asdl.website/asdl-hub

## Status
ASDL Hub is currently under active development.
Expect breaking changes while the project approaches its first stable release.

## License
See [LICENSE](https://github.com/asadullahbro/ASDL-Hub/blob/main/LICENSE).
