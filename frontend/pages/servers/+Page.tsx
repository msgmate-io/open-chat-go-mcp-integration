import { useState } from "react";
import useSWR from "swr";
import { IntegrationPageShell } from "@open-chat-go/ui";
import { fetcher } from "@/lib/utils";
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  Input,
  Text,
  TextTypes,
} from "@open-chat-go/ui";

type MCPServerRow = {
  name: string;
  config: Record<string, unknown>;
  enabled: boolean;
  has_auth_data: boolean;
  auth_mode: string;
  auth_connected: boolean;
  auth_pending: boolean;
  created_at_unix: number;
  updated_at_unix: number;
};

type MCPServersResponse = {
  rows: MCPServerRow[];
};

type MCPAuthConnectField = {
  key: string;
  label: string;
  type?: string;
  required?: boolean;
  placeholder?: string;
  help_text?: string;
  default_value?: string;
};

type OAuthConfigDialogState = {
  open: boolean;
  row: MCPServerRow | null;
  fields: MCPAuthConnectField[];
  values: Record<string, string>;
  error: string;
  isSaving: boolean;
};

type BearerDialogState = {
  open: boolean;
  row: MCPServerRow | null;
  token: string;
  error: string;
  isSaving: boolean;
};

function authConfigFromRow(row: MCPServerRow): Record<string, unknown> {
  if (row.config && typeof row.config.auth === "object" && row.config.auth) {
    return row.config.auth as Record<string, unknown>;
  }
  return {};
}

function hostDefaultsForRow(
  row: MCPServerRow,
): {
  defaultRedirect: string;
  defaultAuthorizeURL: string;
  defaultTokenURL: string;
  defaultScopes: string[];
} {
  const configURL = typeof row.config?.url === "string" ? row.config.url : "";
  const configHost = configURL.toLowerCase();
  const isFigmaHost = configHost.includes("mcp.figma.com");
  const isGoogleDriveHost = configHost.includes("drivemcp.googleapis.com");
  const dynamicCallback = `${window.location.origin}/callback`;

  return {
    defaultRedirect:
      isFigmaHost || isGoogleDriveHost
        ? dynamicCallback
        : `${window.location.origin}/integrations/mcp/servers`,
    defaultAuthorizeURL: isGoogleDriveHost
      ? "https://accounts.google.com/o/oauth2/v2/auth"
      : "https://www.figma.com/oauth",
    defaultTokenURL: isGoogleDriveHost
      ? "https://oauth2.googleapis.com/token"
      : "https://api.figma.com/v1/oauth/token",
    defaultScopes: isGoogleDriveHost
      ? [
          "https://www.googleapis.com/auth/drive.readonly",
          "https://www.googleapis.com/auth/drive.file",
        ]
      : ["file_content:read"],
  };
}

function readOAuthConnectFields(auth: Record<string, unknown>): MCPAuthConnectField[] {
  const raw = auth.connect_fields;
  if (!Array.isArray(raw)) {
    return [];
  }

  const out: MCPAuthConnectField[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const field = item as Record<string, unknown>;
    const key = typeof field.key === "string" ? field.key.trim() : "";
    if (!key) {
      continue;
    }
    const label =
      typeof field.label === "string" && field.label.trim()
        ? field.label.trim()
        : key;
    out.push({
      key,
      label,
      type: typeof field.type === "string" ? field.type.trim() : "text",
      required: Boolean(field.required),
      placeholder:
        typeof field.placeholder === "string" ? field.placeholder : undefined,
      help_text: typeof field.help_text === "string" ? field.help_text : undefined,
      default_value:
        typeof field.default_value === "string" ? field.default_value : undefined,
    });
  }

  return out;
}

function defaultOAuthConnectFields(): MCPAuthConnectField[] {
  return [
    {
      key: "client_id",
      label: "OAuth Client ID",
      type: "text",
      required: true,
    },
    {
      key: "client_secret",
      label: "OAuth Client Secret",
      type: "password",
      required: false,
      help_text: "Optional for PKCE public clients.",
    },
    {
      key: "redirect_uri",
      label: "OAuth Redirect URI",
      type: "url",
      required: true,
    },
  ];
}

function formatUnixTimestamp(value: number): string {
  if (!value) {
    return "-";
  }
  try {
    return new Date(value * 1000).toLocaleString();
  } catch {
    return "-";
  }
}

export default function MCPServersPage() {
  const { data, isLoading, error, mutate } = useSWR<MCPServersResponse>(
    "/api/v1/integrations/mcp/servers",
    fetcher,
  );
  const [statusByServer, setStatusByServer] = useState<Record<string, string>>({});
  const [oauthDialog, setOAuthDialog] = useState<OAuthConfigDialogState>({
    open: false,
    row: null,
    fields: [],
    values: {},
    error: "",
    isSaving: false,
  });
  const [bearerDialog, setBearerDialog] = useState<BearerDialogState>({
    open: false,
    row: null,
    token: "",
    error: "",
    isSaving: false,
  });

  const rows = data?.rows ?? [];

  const setServerStatus = (serverName: string, message: string) => {
    setStatusByServer((current) => ({ ...current, [serverName]: message }));
  };

  const clearServerStatus = (serverName: string) => {
    setStatusByServer((current) => {
      const next = { ...current };
      delete next[serverName];
      return next;
    });
  };

  const discover = async (serverName: string) => {
    clearServerStatus(serverName);
    const response = await fetch(
      `/api/v1/integrations/mcp/servers/${encodeURIComponent(serverName)}/discover`,
      {
        method: "POST",
        credentials: "include",
      },
    );
    if (response.ok) {
      const payload = (await response.json()) as { count?: number };
      setServerStatus(serverName, `Loaded ${payload.count ?? 0} tools.`);
      await mutate();
      return;
    }
    const message = await response.text();
    setServerStatus(serverName, message || "Discovery failed.");
  };

  const remove = async (serverName: string) => {
    const confirmed = window.confirm(`Delete MCP server '${serverName}'?`);
    if (!confirmed) {
      return;
    }
    const response = await fetch(
      `/api/v1/integrations/mcp/servers/${encodeURIComponent(serverName)}`,
      {
        method: "DELETE",
        credentials: "include",
      },
    );
    if (response.ok) {
      clearServerStatus(serverName);
      await mutate();
      return;
    }
    const message = await response.text();
    setServerStatus(serverName, message || "Delete failed.");
  };

  const openOAuthConfigDialog = (row: MCPServerRow, initialError = "") => {
    const currentAuth = authConfigFromRow(row);
    const hostDefaults = hostDefaultsForRow(row);
    const fieldDefs = readOAuthConnectFields(currentAuth);
    const fields = fieldDefs.length > 0 ? fieldDefs : defaultOAuthConnectFields();

    const initialValues: Record<string, string> = {};
    for (const field of fields) {
      const rawValue = currentAuth[field.key];
      if (typeof rawValue === "string") {
        initialValues[field.key] = rawValue;
        continue;
      }
      if (field.default_value) {
        initialValues[field.key] = field.default_value;
        continue;
      }
      if (field.key === "redirect_uri") {
        initialValues[field.key] = hostDefaults.defaultRedirect;
      }
    }

    setOAuthDialog({
      open: true,
      row,
      fields,
      values: initialValues,
      error: initialError,
      isSaving: false,
    });
  };

  const saveOAuthConfig = async () => {
    const row = oauthDialog.row;
    if (!row) {
      return;
    }

    const values: Record<string, string> = {};
    for (const field of oauthDialog.fields) {
      const value = (oauthDialog.values[field.key] || "").trim();
      values[field.key] = value;
      if (field.required && !value) {
        setOAuthDialog((current) => ({
          ...current,
          error: `${field.label} is required.`,
        }));
        return;
      }
    }

    setOAuthDialog((current) => ({ ...current, error: "", isSaving: true }));

    const currentAuth = authConfigFromRow(row);
    const hostDefaults = hostDefaultsForRow(row);
    const nextConfig: Record<string, unknown> = {
      ...row.config,
      auth: {
        ...currentAuth,
        ...values,
        mode: "oauth2",
        redirect_uri:
          values.redirect_uri ||
          (typeof currentAuth.redirect_uri === "string" &&
          currentAuth.redirect_uri.trim()
            ? currentAuth.redirect_uri
            : hostDefaults.defaultRedirect),
        authorize_url:
          typeof currentAuth.authorize_url === "string" &&
          currentAuth.authorize_url.trim()
            ? currentAuth.authorize_url
            : hostDefaults.defaultAuthorizeURL,
        token_url:
          typeof currentAuth.token_url === "string" && currentAuth.token_url.trim()
            ? currentAuth.token_url
            : hostDefaults.defaultTokenURL,
        use_pkce: currentAuth.use_pkce ?? true,
        scopes: currentAuth.scopes ?? hostDefaults.defaultScopes,
      },
    };

    const response = await fetch(
      `/api/v1/integrations/mcp/servers/${encodeURIComponent(row.name)}`,
      {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: row.name,
          enabled: row.enabled,
          config: nextConfig,
        }),
      },
    );

    if (!response.ok) {
      const message = await response.text();
      setOAuthDialog((current) => ({
        ...current,
        isSaving: false,
        error: message || "Failed to update OAuth config.",
      }));
      return;
    }

    await mutate();
    setOAuthDialog({
      open: false,
      row: null,
      fields: [],
      values: {},
      error: "",
      isSaving: false,
    });
    setServerStatus(row.name, "OAuth settings saved. Click Connect to continue.");
  };

  const openBearerDialog = (row: MCPServerRow, initialError = "") => {
    setBearerDialog({
      open: true,
      row,
      token: "",
      error: initialError,
      isSaving: false,
    });
  };

  const saveBearerToken = async () => {
    const row = bearerDialog.row;
    if (!row) {
      return;
    }

    const token = bearerDialog.token.trim();
    if (!token) {
      setBearerDialog((current) => ({
        ...current,
        error: "Bearer token is required.",
      }));
      return;
    }

    setBearerDialog((current) => ({ ...current, error: "", isSaving: true }));
    const completeResponse = await fetch(
      `/api/v1/integrations/mcp/servers/${encodeURIComponent(row.name)}/auth/complete`,
      {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ bearer_token: token }),
      },
    );

    if (!completeResponse.ok) {
      const message = await completeResponse.text();
      setBearerDialog((current) => ({
        ...current,
        isSaving: false,
        error: message || "Failed to complete auth flow.",
      }));
      return;
    }

    await mutate();
    setBearerDialog({
      open: false,
      row: null,
      token: "",
      error: "",
      isSaving: false,
    });
    setServerStatus(row.name, "Bearer token saved.");
  };

  const connect = async (row: MCPServerRow) => {
    const startResponse = await fetch(
      `/api/v1/integrations/mcp/servers/${encodeURIComponent(row.name)}/auth/start`,
      {
        method: "POST",
        credentials: "include",
      },
    );
    if (!startResponse.ok) {
      const message = await startResponse.text();
      const needsClientID =
        row.auth_mode === "oauth2" &&
        message.toLowerCase().includes("client_id") &&
        message.toLowerCase().includes("required");
      if (needsClientID) {
        openOAuthConfigDialog(row, "Configure OAuth client settings before connecting.");
        return;
      }
      setServerStatus(row.name, message || "Failed to start auth flow.");
      return;
    }

    const startPayload = (await startResponse.json()) as {
      mode?: string;
      authorize_url?: string;
    };
    const mode = String(startPayload.mode || row.auth_mode || "none").toLowerCase();

    if (mode === "none") {
      setServerStatus(row.name, "No authentication required. Connection is ready.");
      await mutate();
      return;
    }

    if (mode === "bearer_token") {
      openBearerDialog(row, "Paste bearer token to complete authentication.");
      return;
    }

    if (mode === "oauth2") {
      const authorizeURL = String(startPayload.authorize_url || "").trim();
      if (!authorizeURL) {
        setServerStatus(row.name, "Missing authorize_url from auth/start response.");
        return;
      }
      try {
        window.localStorage.setItem("mcp_auth_backend_origin", window.location.origin);
      } catch {
        // ignore storage failures
      }
      window.open(authorizeURL, "_blank", "noopener,noreferrer");
      setServerStatus(
        row.name,
        "OAuth window opened. Approve access to complete connection.",
      );
      return;
    }

    setServerStatus(row.name, `Unsupported auth mode: ${mode}`);
  };

  const clearAuth = async (serverName: string) => {
    const response = await fetch(
      `/api/v1/integrations/mcp/servers/${encodeURIComponent(serverName)}/auth/clear`,
      {
        method: "POST",
        credentials: "include",
      },
    );
    if (response.ok) {
      setServerStatus(serverName, "Authentication data cleared.");
      await mutate();
      return;
    }
    const message = await response.text();
    setServerStatus(serverName, message || "Failed to clear auth data.");
  };

  return (
    <IntegrationPageShell>
      <div className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <Text type={TextTypes.Heading5} tag="h1" bold>
              MCP Servers
            </Text>
            <Text type={TextTypes.Body6} color="muted">
              Manage registered MCP servers for this account.
            </Text>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <a href="/integrations/mcp/templates">
              <Button variant="outline">Browse Templates</Button>
            </a>
            <a href="/integrations/mcp/servers/add">
              <Button>Add Server</Button>
            </a>
          </div>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Registered servers</CardTitle>
            <CardDescription>{rows.length} total</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {isLoading ? <div className="h-24 animate-pulse rounded bg-muted" /> : null}
            {error ? (
              <Text type={TextTypes.Body6} color="destructive">
                Failed to load MCP servers.
              </Text>
            ) : null}
            {!isLoading && !error && rows.length === 0 ? (
              <Text type={TextTypes.Body6} color="muted">
                No MCP servers registered yet.
              </Text>
            ) : null}
            {!isLoading && !error
              ? rows.map((row) => (
                  <div
                    key={row.name}
                    className="space-y-3 rounded-lg border border-border/70 bg-card/80 p-3"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <Text type={TextTypes.Body5} bold>
                          {row.name}
                        </Text>
                        <Badge variant={row.enabled ? "secondary" : "outline"}>
                          {row.enabled ? "Enabled" : "Disabled"}
                        </Badge>
                        <Badge variant={row.has_auth_data ? "secondary" : "outline"}>
                          {row.has_auth_data ? "Auth set" : "No auth"}
                        </Badge>
                        <Badge variant="outline">Mode: {row.auth_mode || "none"}</Badge>
                        <Badge variant={row.auth_connected ? "secondary" : "outline"}>
                          {row.auth_connected
                            ? "Connected"
                            : row.auth_pending
                              ? "Pending"
                              : "Not connected"}
                        </Badge>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        <Button size="sm" variant="outline" onClick={() => connect(row)}>
                          {row.auth_connected ? "Reconnect" : "Connect"}
                        </Button>
                        {row.auth_mode === "oauth2" ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => openOAuthConfigDialog(row)}
                          >
                            Configure OAuth
                          </Button>
                        ) : null}
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => clearAuth(row.name)}
                        >
                          Disconnect
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => discover(row.name)}
                        >
                          Discover
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => remove(row.name)}>
                          Delete
                        </Button>
                      </div>
                    </div>
                    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                      <span>Created: {formatUnixTimestamp(row.created_at_unix)}</span>
                      <span>Updated: {formatUnixTimestamp(row.updated_at_unix)}</span>
                    </div>
                    {statusByServer[row.name] ? (
                      <Text
                        type={TextTypes.Body7}
                        color={statusByServer[row.name].toLowerCase().startsWith("loaded") || statusByServer[row.name].toLowerCase().includes("ready") || statusByServer[row.name].toLowerCase().includes("opened") || statusByServer[row.name].toLowerCase().includes("saved") || statusByServer[row.name].toLowerCase().includes("cleared") ? "muted" : "destructive"}
                      >
                        {statusByServer[row.name]}
                      </Text>
                    ) : null}
                    <pre className="max-h-56 overflow-auto rounded bg-muted/30 p-2 text-xs">
                      {JSON.stringify(row.config ?? {}, null, 2)}
                    </pre>
                  </div>
                ))
              : null}
          </CardContent>
        </Card>
      </div>

      <Dialog
        open={oauthDialog.open}
        onOpenChange={(open) =>
          setOAuthDialog((current) => ({
            ...current,
            open,
            error: open ? current.error : "",
          }))
        }
      >
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>Configure OAuth</DialogTitle>
            <DialogDescription>
              Fill required OAuth fields for {oauthDialog.row?.name || "this MCP server"}.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            {oauthDialog.fields.map((field) => {
              const inputType =
                field.type === "password" || field.type === "url"
                  ? field.type
                  : "text";
              return (
                <div key={field.key} className="space-y-1">
                  <Text type={TextTypes.Body7} bold>
                    {field.label}
                    {field.required ? " *" : ""}
                  </Text>
                  <Input
                    type={inputType}
                    value={oauthDialog.values[field.key] || ""}
                    placeholder={field.placeholder || ""}
                    onChange={(event) =>
                      setOAuthDialog((current) => ({
                        ...current,
                        values: {
                          ...current.values,
                          [field.key]: event.target.value,
                        },
                      }))
                    }
                  />
                  {field.help_text ? (
                    <Text type={TextTypes.Body7} color="muted">
                      {field.help_text}
                    </Text>
                  ) : null}
                </div>
              );
            })}

            {oauthDialog.error ? (
              <Text type={TextTypes.Body7} color="destructive">
                {oauthDialog.error}
              </Text>
            ) : null}

            <div className="flex items-center gap-2 pt-1">
              <Button onClick={saveOAuthConfig} disabled={oauthDialog.isSaving}>
                {oauthDialog.isSaving ? "Saving..." : "Save OAuth Settings"}
              </Button>
              <Button
                variant="outline"
                onClick={() =>
                  setOAuthDialog({
                    open: false,
                    row: null,
                    fields: [],
                    values: {},
                    error: "",
                    isSaving: false,
                  })
                }
              >
                Cancel
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog
        open={bearerDialog.open}
        onOpenChange={(open) =>
          setBearerDialog((current) => ({
            ...current,
            open,
            error: open ? current.error : "",
          }))
        }
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Bearer Token</DialogTitle>
            <DialogDescription>
              Paste bearer token for {bearerDialog.row?.name || "this MCP server"}.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <Input
              type="password"
              value={bearerDialog.token}
              placeholder="Bearer token"
              onChange={(event) =>
                setBearerDialog((current) => ({
                  ...current,
                  token: event.target.value,
                }))
              }
            />
            {bearerDialog.error ? (
              <Text type={TextTypes.Body7} color="destructive">
                {bearerDialog.error}
              </Text>
            ) : null}
            <div className="flex flex-wrap items-center gap-2">
              <Button onClick={saveBearerToken} disabled={bearerDialog.isSaving}>
                {bearerDialog.isSaving ? "Saving..." : "Save Token"}
              </Button>
              <Button
                variant="outline"
                onClick={() =>
                  setBearerDialog({
                    open: false,
                    row: null,
                    token: "",
                    error: "",
                    isSaving: false,
                  })
                }
              >
                Cancel
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </IntegrationPageShell>
  );
}
