# MCP Integration

The MCP integration lets each user register one or more remote MCP servers and use discovered MCP tools in bot chats.

## What it provides

- MCP server CRUD APIs (`/api/v1/integrations/mcp/servers...`)
- OAuth/token connect flow for supported servers
- Tool discovery and namespaced tool snapshots (`mcp:<integration>:<tool>`)
- Frontend pages for server management under `/integrations/mcp/servers`

## Typical usage

1. Open `/integrations/mcp/servers`
2. Add a server with HTTPS URL and transport config
3. Connect auth if required
4. Discover tools
5. Attach integration names in bot config `default_shared_config.integrations`

Once attached, discovered tools are merged into the bot tool list.
