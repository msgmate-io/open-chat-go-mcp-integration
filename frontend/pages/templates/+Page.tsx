import { useState } from "react";
import useSWR from "swr";
import Markdown from "react-markdown";
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
  logo_data_url?: string;
  config: Record<string, unknown>;
  readme_markdown: string;
};

type MCPTemplatesResponse = {
  rows: MCPTemplateRow[];
};

export default function MCPTemplatesPage() {
  const { data, isLoading, error } = useSWR<MCPTemplatesResponse>(
    "/api/v1/integrations/mcp/templates",
    fetcher,
  );
  const rows = data?.rows ?? [];
  const [expandedReadmeByID, setExpandedReadmeByID] = useState<Record<string, boolean>>({});
  const [expandedConfigByID, setExpandedConfigByID] = useState<Record<string, boolean>>({});

  return (
    <IntegrationPageShell maxWidthClassName="max-w-6xl">
      <div className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <Text type={TextTypes.Heading5} tag="h1" bold>
              MCP Templates
            </Text>
            <Text type={TextTypes.Body6} color="muted">
              Start from prebuilt MCP configs. Expand setup notes or config per template.
            </Text>
          </div>
          <a href="/integrations/mcp/servers">
            <Button variant="outline" type="button">
              Back to Servers
            </Button>
          </a>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Template library</CardTitle>
            <CardDescription>{rows.length} available</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {isLoading ? <div className="h-24 animate-pulse rounded bg-muted" /> : null}
            {error ? (
              <Text type={TextTypes.Body6} color="destructive">
                Failed to load MCP templates.
              </Text>
            ) : null}
            {!isLoading && !error && rows.length === 0 ? (
              <Text type={TextTypes.Body6} color="muted">
                No MCP templates available.
              </Text>
            ) : null}

            {!isLoading && !error ? (
              <div className="space-y-2">
                {rows.map((template) => {
                  const readmeExpanded = !!expandedReadmeByID[template.id];
                  const configExpanded = !!expandedConfigByID[template.id];
                  return (
                    <Card key={template.id} className="border-border/70 bg-card/80">
                      <CardContent className="space-y-2 p-3">
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex min-w-0 items-center gap-2 text-sm">
                              {template.logo_data_url ? (
                                <div className="flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded border border-border/60 bg-background p-1">
                                  <img
                                    src={template.logo_data_url}
                                    alt={`${template.display_name} logo`}
                                    className="h-full w-full object-contain"
                                  />
                                </div>
                              ) : null}
                              <CardTitle className="truncate text-sm">
                                {template.display_name}
                              </CardTitle>
                              {template.is_recommended ? (
                                <Badge variant="secondary">Recommended</Badge>
                              ) : null}
                              <Badge variant="outline" className="shrink-0">
                                {template.id}
                              </Badge>
                            </div>
                            <div className="mt-1 text-xs text-muted-foreground">
                              <span className="line-clamp-1">
                                {template.description || "No description provided."}
                              </span>
                            </div>
                            <div className="mt-1 flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
                              <span>Default: {template.default_server_name}</span>
                              {template.tags?.length
                                ? template.tags.map((tag) => (
                                    <Badge key={`${template.id}-${tag}`} variant="outline">
                                      {tag}
                                    </Badge>
                                  ))
                                : null}
                            </div>
                          </div>
                          <a
                            href={`/integrations/mcp/servers/add?template=${encodeURIComponent(template.id)}`}
                          >
                            <Button size="sm" className="h-8 px-3">
                              Use Template
                            </Button>
                          </a>
                        </div>

                        <div className="flex items-center gap-2 pt-1">
                          <Button
                            size="sm"
                            variant="outline"
                            className="h-7 rounded-full px-3 text-xs"
                            type="button"
                            onClick={() =>
                              setExpandedReadmeByID((prev) => ({
                                ...prev,
                                [template.id]: !prev[template.id],
                              }))
                            }
                          >
                            {readmeExpanded ? "Hide README" : "Show README"}
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            className="h-7 rounded-full px-3 text-xs"
                            type="button"
                            onClick={() =>
                              setExpandedConfigByID((prev) => ({
                                ...prev,
                                [template.id]: !prev[template.id],
                              }))
                            }
                          >
                            {configExpanded ? "Hide Config" : "Show Config"}
                          </Button>
                        </div>

                        {readmeExpanded ? (
                          <div className="rounded-md border border-border/60 bg-muted/20 p-3">
                            <Text type={TextTypes.Body7} bold>
                              Setup notes
                            </Text>
                            <div className="chat-markdown mt-2 text-sm">
                              <Markdown>{template.readme_markdown || "No README provided."}</Markdown>
                            </div>
                          </div>
                        ) : null}

                        {configExpanded ? (
                          <div className="rounded-md border border-border/60 bg-muted/20 p-3">
                            <Text type={TextTypes.Body7} bold>
                              Template config preview
                            </Text>
                            <pre className="mt-2 max-h-56 overflow-auto rounded bg-muted/40 p-2 text-xs">
                              {JSON.stringify(template.config ?? {}, null, 2)}
                            </pre>
                          </div>
                        ) : null}
                      </CardContent>
                    </Card>
                  );
                })}
              </div>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </IntegrationPageShell>
  );
}
