# Figma MCP Template

Use this template to quickly register the hosted Figma MCP server.

## What you need

- A Figma OAuth app with a client ID (and optional client secret for confidential clients)
- Redirect URI configured in your Figma OAuth app (default in this template: `http://localhost:3000/callback`)

## Setup steps

1. Create an OAuth app in Figma and copy the client credentials.
2. Open the MCP add-server page with this template prefilled.
3. Paste `client_id` and `client_secret` into `config.auth`.
4. Adjust `redirect_uri` and `scopes` if your app uses different values.
5. Create the server, then click **Connect** from the MCP server list.
6. Complete the OAuth flow and return to Open Chat Go.

## Notes

- The template uses `transport: http_streamable` and points to `https://mcp.figma.com/mcp`.
- `use_pkce` is enabled by default.
- If your OAuth provider settings change, update the config before connecting.
