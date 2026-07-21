# Google Workspace Sheets MCP Template

Use this template to register the hosted Google Sheets MCP server and enable spreadsheet read/write tooling.

## What you need

- A Google Cloud project with access to Google Workspace MCP preview
- OAuth client credentials (client ID and client secret)
- Redirect URI configured in your OAuth client (default in this template: `http://localhost:1984/callback`)

## Setup steps

1. Enable required APIs in your Google Cloud project:

```bash
gcloud services enable sheets.googleapis.com sheetsmcp.googleapis.com --project=PROJECT_ID
```

2. Configure the OAuth consent screen (internal if available, otherwise external + test users).
3. Create an OAuth 2.0 client of type **Web application**.
4. Add your redirect URI (for example `http://localhost:1984/callback`).
5. Open MCP template catalog at `/integrations/mcp/templates` and select this template.
6. Paste `client_id` and `client_secret` into `config.auth`.
7. Create the server and click **Connect** in the MCP server list.
8. Approve Sheets access and return to Open Chat Go.
9. Click **Discover** to refresh the MCP tool list after auth.

## Scopes included by this template

- `https://www.googleapis.com/auth/spreadsheets`
- `https://www.googleapis.com/auth/spreadsheets.readonly`
- `https://www.googleapis.com/auth/drive.file`
- `https://www.googleapis.com/auth/drive.readonly`

## Notes

- This template targets Google Sheets MCP endpoint `https://sheetsmcp.googleapis.com/mcp/v1`.
- If OAuth settings or scopes change, reconnect the server to refresh tokens.
- Keep secrets out of source control.
