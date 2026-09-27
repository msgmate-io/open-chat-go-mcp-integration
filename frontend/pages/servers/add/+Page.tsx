import { useEffect, useMemo, useState } from "react";
import useSWR from "swr";
import { IntegrationPageShell } from "@open-chat-go/ui";
import { fetcher } from "@/lib/utils";
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Text,
  TextTypes,
} from "@open-chat-go/ui";

type MCPTemplateRow = {
  id: string;
  display_name: string;
  description: string;
  default_server_name: string;
  tags: string[];
  is_recommended: boolean;
  config: Record<string, unknown>;
  readme_markdown: string;
};

type MCPTemplatesResponse = {
  rows: MCPTemplateRow[];
};

const fallbackFigmaConfig = {
  transport: "http_streamable",
  url: "https://mcp.figma.com/mcp",
  request_timeout_seconds: 25,
  auth: {
    mode: "oauth2",
    authorize_url: "https://www.figma.com/oauth",
    token_url: "https://api.figma.com/v1/oauth/token",
    client_id: "",
    client_secret: "",
    redirect_uri: "http://localhost:3000/callback",
    scopes: ["file_content:read"],
    use_pkce: true,
  },
};

function parseObjectJSON(raw: string, fieldName: string): Record<string, unknown> {
  const parsed = JSON.parse(raw);
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
    throw new Error(`${fieldName} must be a JSON object.`);
  }
  return parsed as Record<string, unknown>;
}

export default function MCPAddServerPage() {
  const selectedTemplateID = useMemo(() => {
    if (typeof window === "undefined") {
      return "";
    }
    const params = new URLSearchParams(window.location.search);
    return (params.get("template") || "").trim().toLowerCase();
  }, []);

  const { data: templatesData, error: templatesError } = useSWR<MCPTemplatesResponse>(
    "/api/v1/integrations/mcp/templates",
    fetcher,
  );

  const [name, setName] = useState("figma");
  const [enabled, setEnabled] = useState(true);
  const [configText, setConfigText] = useState(JSON.stringify(fallbackFigmaConfig, null, 2));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [status, setStatus] = useState<string>("");
  const [templatePrefilled, setTemplatePrefilled] = useState(false);
  const [nameTouched, setNameTouched] = useState(false);
  const [configTouched, setConfigTouched] = useState(false);

  useEffect(() => {
    if (templatePrefilled || !selectedTemplateID || !templatesData?.rows) {
      return;
    }
    const selectedTemplate = templatesData.rows.find(
      (item) => item.id.toLowerCase() === selectedTemplateID,
    );
    if (!selectedTemplate) {
      setStatus(`Template '${selectedTemplateID}' was not found.`);
      setTemplatePrefilled(true);
      return;
    }

    if (!nameTouched) {
      setName(selectedTemplate.default_server_name || selectedTemplate.id);
    }
    if (!configTouched) {
      setConfigText(JSON.stringify(selectedTemplate.config, null, 2));
    }
    setStatus(`Loaded template: ${selectedTemplate.display_name}`);
    setTemplatePrefilled(true);
  }, [
    configTouched,
    nameTouched,
    selectedTemplateID,
    templatePrefilled,
    templatesData?.rows,
  ]);

  useEffect(() => {
    if (!selectedTemplateID || !templatesError) {
      return;
    }
    setStatus("Failed to load templates for prefill. Using fallback config.");
  }, [selectedTemplateID, templatesError]);

  const submit = async () => {
    setStatus("");
    setIsSubmitting(true);
    try {
      const payload = {
        name,
        enabled,
        config: parseObjectJSON(configText, "config"),
      };

      const response = await fetch("/api/v1/integrations/mcp/servers", {
        method: "POST",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(payload),
      });

      if (!response.ok) {
        const message = await response.text();
        throw new Error(message || "Failed to add server.");
      }

      setStatus("Server created. Redirecting to server list...");
      window.setTimeout(() => {
        window.location.href = "/integrations/mcp/servers";
      }, 400);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Failed to add server.");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <IntegrationPageShell maxWidthClassName="max-w-4xl">
      <div className="space-y-4">
        <div>
          <Text type={TextTypes.Heading5} tag="h1" bold>
            Add MCP Server
          </Text>
          <Text type={TextTypes.Body6} color="muted">
            Register an owner-scoped MCP server endpoint.
          </Text>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>New server</CardTitle>
            <CardDescription>
              The server is validated with a connection check during creation.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <Input
              value={name}
              onChange={(event) => {
                setNameTouched(true);
                setName(event.target.value);
              }}
              placeholder="server name (slug)"
            />

            <label className="flex items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(event) => setEnabled(event.target.checked)}
              />
              Enabled
            </label>

            <div className="space-y-1">
              <Text type={TextTypes.Body6} bold>
                Config (JSON)
              </Text>
              <textarea
                className="h-52 w-full rounded-md border border-border bg-background p-3 font-mono text-xs"
                value={configText}
                onChange={(event) => {
                  setConfigTouched(true);
                  setConfigText(event.target.value);
                }}
              />
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <a href="/integrations/mcp/templates">
                <Button variant="outline" type="button">
                  Browse Templates
                </Button>
              </a>
              <Button onClick={submit} disabled={isSubmitting}>
                {isSubmitting ? "Creating..." : "Create server"}
              </Button>
              <a href="/integrations/mcp/servers">
                <Button variant="outline" type="button">
                  Back
                </Button>
              </a>
            </div>

            {status ? (
              <Text
                type={TextTypes.Body6}
                color={status.toLowerCase().includes("failed") || status.toLowerCase().includes("error") ? "destructive" : "muted"}
              >
                {status}
              </Text>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </IntegrationPageShell>
  );
}
