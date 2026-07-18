# Google Workspace Drive MCP Template

Use this template to register the hosted Google Drive MCP server and pull content from Google Docs files stored in Drive.

## What you need

- A Google Cloud project with access to Google Workspace MCP preview
- OAuth client credentials (client ID and client secret)
- Redirect URI configured in your OAuth client (default in this template: `http://localhost:1984/callback`)

## Setup steps

1. Enable required APIs in your Google Cloud project:

```bash
gcloud services enable drive.googleapis.com drivemcp.googleapis.com --project=PROJECT_ID
```

2. Configure the OAuth consent screen (internal if available, otherwise external + test users).
3. Create an OAuth 2.0 client of type **Web application**.
4. Add your redirect URI (for example `http://localhost:1984/callback`).
5. Open MCP template catalog at `/integrations/mcp/templates` and select this template.
6. Paste `client_id` and `client_secret` into `config.auth`.
7. Create the server and click **Connect** in the MCP server list.
8. Approve Drive access and return to Open Chat Go.

## Scopes for Google Docs access

- Minimum read scope:
  - `https://www.googleapis.com/auth/drive.readonly`
- Optional write scope (if you need uploads/edits):
  - `https://www.googleapis.com/auth/drive.file`

## German quick setup (Kurzfassung)

1. APIs aktivieren: `drive.googleapis.com` und `drivemcp.googleapis.com`.
2. OAuth-Zustimmungsbildschirm einrichten (intern oder extern mit Testnutzern).
3. OAuth-Client als Webanwendung erstellen.
4. Redirect-URI in der Client-Konfiguration hinterlegen.
5. Scope fuer Docs-Lesezugriff setzen: `https://www.googleapis.com/auth/drive.readonly`.

## Notes

- This template targets Google Drive MCP endpoint `https://drivemcp.googleapis.com/mcp/v1`.
- If your OAuth settings change, update `config.auth` before reconnecting.
- Keep secrets out of source control.
