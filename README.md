# MCP Integration

The MCP integration lets each user register one or more remote MCP servers and use discovered MCP tools in bot chats.

## What it provides

- MCP server CRUD APIs (`/api/v1/integrations/mcp/servers...`)
- MCP template library API (`/api/v1/integrations/mcp/templates`)
- OAuth/token connect flow for supported servers
- Tool discovery and namespaced tool snapshots (`mcp:<integration>:<tool>`)
- Frontend pages for server management under `/integrations/mcp/servers`
- Frontend template catalog under `/integrations/mcp/templates`

## Built-in templates

- Figma MCP (hosted OAuth2)
- Playwright MCP (local no-auth)
- Google Workspace Drive MCP (hosted OAuth2, Docs/Drive read access)

## Template-driven auth fields

- OAuth templates can provide `config.auth.connect_fields` to define UI input fields.
- The MCP servers page renders these fields dynamically in the connect dialog.
- If `connect_fields` is missing, the UI falls back to generic OAuth defaults (`client_id`, `client_secret`, `redirect_uri`).

## Typical usage

1. Open `/integrations/mcp/servers`
2. Add a server with HTTPS URL and transport config
3. Connect auth if required
4. Discover tools
5. Attach integration names in bot config `default_shared_config.integrations`

Once attached, discovered tools are merged into the bot tool list.
