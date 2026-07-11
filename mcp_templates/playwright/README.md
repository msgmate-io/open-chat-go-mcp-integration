# Playwright MCP (Local) Template

This template connects Open Chat Go to a Playwright MCP server running on your local machine.

## Start Playwright MCP

Run this on your host machine:

```bash
npx @playwright/mcp@latest --port 8931 --host 0.0.0.0 --allowed-hosts '*'
```

You should see output similar to:

- `Listening on http://localhost:8931`
- MCP endpoint at `http://localhost:8931/mcp`

## Important container note

When Open Chat Go backend runs in Docker, `localhost` inside the container points to the container itself, not your host.

This template sets `resolve_localhost_to_host_docker_internal: true`, so backend calls to `localhost` are redirected to `host.docker.internal` automatically.

If Playwright MCP is started without `--host 0.0.0.0`, it usually binds to loopback only and is unreachable from Docker containers.

## Setup steps

1. Start Playwright MCP locally with the command above.
2. Choose this template from `/integrations/mcp/templates`.
3. Create the server.
4. Click **Discover** on the server list.
5. Attach integration tools to your bot and start chatting.

## Troubleshooting

- If discovery fails, confirm port `8931` is reachable from the backend container.
- On Linux Docker setups, ensure `host.docker.internal` is available (or update the URL to a reachable host address).
