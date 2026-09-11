---
title: Web dashboard in Docker
description: Build and run the OpenUsage web dashboard and telemetry daemon in a container.
---

`Dockerfile.web` packages the React dashboard, the OpenUsage binary, and the
telemetry daemon in one image. The container listens on port `8787` and stores
settings, credentials, telemetry history, and spool files below `/data`.

## Build

```bash
docker build -f Dockerfile.web -t openusage-web:local .
```

The image builds the Vite dashboard and embeds its runtime assets in the Go
binary. It runs the daemon and web server as one container workload.

## Run

Set a web access token before starting the image. Public binding inside the
container is rejected without this token.

```bash
TOKEN="$(openssl rand -hex 32)"
docker run --rm \
  --name openusage-web \
  -p 8787:8787 \
  -e OPENUSAGE_WEB_TOKEN="$TOKEN" \
  -v openusage-data:/data \
  openusage-web:local
```

Open:

```text
http://127.0.0.1:8787/
```

Enter `<TOKEN>` in the dashboard. The server keeps it in an HttpOnly cookie
after the first successful request. The token is not placed in the URL or
browser storage. Treat it like a password. Put a TLS reverse proxy in front of
the container if it is reachable from another machine.

## Provider data

Pass provider API keys as container environment variables:

```bash
docker run --rm \
  -p 8787:8787 \
  -e OPENUSAGE_WEB_TOKEN="$TOKEN" \
  -e OPENROUTER_API_KEY="$OPENROUTER_API_KEY" \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -v openusage-data:/data \
  openusage-web:local
```

Container auto-detection sees only files and processes available in the
container. To use local-tool history, mount the required tool data read-only
and configure the corresponding paths in `/data/config/settings.json`.

Browser-session authentication needs access to the host browser's cookie store
and OS secret store. It is not available from the default container. Use the
native local web command for that workflow.

ChatGPT Plus/Pro and Claude Pro/Max do not need a CLI or mounted credential
file. In Settings → Credentials → Subscription sign-in:

1. Choose ChatGPT / Codex or Claude Code.
2. Start authorization and open the provider page.
3. For ChatGPT, enter the displayed device code; OpenUsage detects approval
   automatically. For Claude, paste the code shown after approval.

OpenUsage keeps PKCE and device-flow state in server memory and writes only the
resulting access and refresh tokens to its `credentials.json` file on the
persistent `/data` volume with `0600` permissions. Existing CLI auth JSON can
still be imported from the advanced fallback form.

## Health check

The image exposes port `8787` and checks:

```text
GET /healthz
```

The health endpoint reports web liveness and does not require the access token.
