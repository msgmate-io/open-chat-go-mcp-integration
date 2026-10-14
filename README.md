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

## OAuth redirects and token caching

- The OAuth `redirect_uri` follows the server host: an empty or loopback
  (`localhost`/`127.0.0.1`) `config.auth.redirect_uri` is replaced at
  `auth/start` with `<public-base-url>/callback`, where the public base URL is
  `PUBLIC_BASE_URL` when set and otherwise derived from the request's
  `X-Forwarded-Host`/`Host` + scheme. A non-loopback `redirect_uri` is honoured
  as-is, so deployments can register a custom callback path.
- For providers that support dynamic client registration (eg the hosted Figma
  MCP), `client_id`/`client_secret` may be left empty; a client is registered on
  first connect and the credentials are persisted with the token so later
  refreshes work.
- Completed credentials (including `refresh_token`/`expires_in`) are cached on
  the owner-scoped server row (`auth_data`). When an access token is at/near
  expiry and a `refresh_token` is present, the integration transparently
  refreshes and re-persists it during status/discovery and whenever an owner's
  servers are resolved (eg by the opencode integration), so a previously
  authorized server keeps working without re-authenticating.

## Typical usage

1. Open `/integrations/mcp/servers`
2. Add a server with HTTPS URL and transport config
3. Connect auth if required
4. Discover tools
5. Attach integration names in bot config `default_shared_config.integrations`

Once attached, discovered tools are merged into the bot tool list.

## Bootstrap from open-chat config

MCP servers can be pre-registered at startup so they are usable immediately,
without adding them through the UI first. Two equivalent surfaces feed the same
idempotent bootstrap implementation:

- backend-level `bootstrap.mcp` (`owners`/`owner` + `servers`), and
- integration-owned `integrations.mcp.bootstrap_servers` /
  `integrations.mcp.bootstrap_default_owners` (mapped to
  `OCI_MCP_BOOTSTRAP_SERVERS` / `OCI_MCP_BOOTSTRAP_DEFAULT_OWNERS`).

Every field except `name` is optional. A built-in template id (`template`)
supplies the base config, and `config` is deep-merged on top of it, so an
operator only overrides what they need (typically `auth.client_id` /
`auth.client_secret` / `auth.redirect_uri`). `auth.client_id` and
`auth.client_secret` stay optional; OAuth endpoints are filled from the
template or inferred for known hosts.

Existing rows are left untouched on restart unless `overwrite: true` is set,
which preserves UI-managed configuration and completed OAuth tokens. Optional
`auth_data` can pre-seed tokens (`bearer_token` or `access_token`) and
`enabled` defaults to `true`.

```yaml
bootstrap:
  mcp:
    owners: [admin]
    servers:
      - name: google-drive
        template: google_workspace_drive
        config:
          auth:
            client_id: "$anchors.google_client_id"
            client_secret: "$anchors.google_client_secret"
            redirect_uri: "https://chat.example.com/callback"
      - name: google-sheets
        template: google_workspace_sheets
        config:
          auth:
            client_id: "$anchors.google_client_id"
            client_secret: "$anchors.google_client_secret"
anchors:
  google_client_id: "123-abc.apps.googleusercontent.com"
  google_client_secret: "GOCSPX-..."
```

The same servers can be declared integration-owned instead:

```yaml
integrations:
  mcp:
    bootstrap_default_owners: "admin"
    bootstrap_servers:
      - name: google-drive
        template: google_workspace_drive
        config:
          auth: { client_id: "...", client_secret: "..." }
```

