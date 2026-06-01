# Security Policy

## Scope

This is a personal, self-hosted tool that never transmits your data to any third party. All grocery data and AH OAuth tokens are stored locally on your own machine and are excluded from version control via `.gitignore`.

If you find a security issue in the code itself (e.g. a vulnerability that could affect someone running this locally), please report it as described below.

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, report them via [GitHub's private vulnerability reporting](https://github.com/tijsvdsmagt/appie-insights/security/advisories/new). Include:

- A description of the vulnerability
- Steps to reproduce it
- The potential impact

You can expect an acknowledgement within a few days. Given the personal nature of this project, there is no formal disclosure timeline, but I will aim to address confirmed issues promptly.

## Network exposure

The backend HTTP API (port 8001) has **no authentication** and exposes endpoints that read all stored data and perform destructive actions (e.g. `POST /database/reset`, `DELETE /enrichment`, `POST /logout`). It is intended for single-user, local use only.

The backend container runs with `network_mode: host` (required so the OAuth login proxy, started on a random port, is reachable from the host browser for the callback). A side effect is that port 8001 binds to all of the host's interfaces. **Do not expose this port to untrusted networks or the public internet.** Run the stack only on a machine and network you trust, and keep port 8001 behind a firewall.

## Notes on the AH API

This project accesses the Albert Heijn API through the same endpoints used by the official mobile app. The OAuth token is stored locally: under the Docker setup it lives in the `appie-config` named volume (mounted at `/app/config/appie.json`), and when the backend is run directly it defaults to your user config directory (`~/.config/appie/appie.json` on Linux). The path can be overridden with the `CONFIG_PATH` environment variable. No credentials are ever sent anywhere other than the AH API itself.
