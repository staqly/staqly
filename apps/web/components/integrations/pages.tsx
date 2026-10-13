"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  Blocks,
  CheckCircle2,
  Code2,
  FileCode2,
  KeyRound,
  Plug,
  PlugZap,
  SquareTerminal,
  Terminal,
  Webhook,
  Workflow,
  XCircle,
} from "lucide-react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { CodeBlock, InlineCode } from "@/components/common/code-block";
import { EmptyState, ErrorState } from "@/components/common/error-state";
import { PageContainer, PageHeader } from "@/components/common/page-header";
import { EventTimeline } from "@/components/items/event-timeline";
import { api, errorMessage, type Health, type SessionInfo } from "@/lib/api";
import { API_URL_FROM_ENV } from "@/lib/config";
import { useCollections } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { API_URL, CloudModeCard, DocSection, KeySourceNote, MCP_TOOLS, MCP_URL, useApiKeyValue } from "./shared";
import { useMode } from "@/lib/queries";

// ---------------------------------------------------------------- overview

interface ProbeResult {
  ok: boolean;
  ms: number;
  health?: Health;
  session?: SessionInfo;
  error?: string;
}

async function probe(): Promise<ProbeResult> {
  const start = performance.now();
  try {
    const [health, session] = await Promise.all([api.health(), api.session()]);
    return { ok: true, ms: Math.round(performance.now() - start), health, session };
  } catch (e) {
    return { ok: false, ms: Math.round(performance.now() - start), error: errorMessage(e) };
  }
}

const INTEGRATION_CARDS = [
  { href: "/integrations/api-keys", title: "API keys", text: "Scoped bearer tokens for each agent", Icon: KeyRound },
  { href: "/integrations/api", title: "REST API", text: "curl examples and the OpenAPI spec", Icon: Code2 },
  { href: "/integrations/mcp", title: "MCP server", text: `${MCP_TOOLS.length} tools over HTTP or stdio`, Icon: Plug },
  { href: "/integrations/claude-code", title: "Claude Code", text: "Add staq.ly as an MCP server", Icon: Terminal },
  { href: "/integrations/codex", title: "Codex", text: "Configure the Codex CLI", Icon: FileCode2 },
  { href: "/integrations/opencode", title: "OpenCode", text: "Add staq.ly to opencode.json", Icon: SquareTerminal },
  { href: "/integrations/n8n", title: "n8n", text: "HTTP Request nodes and signed webhooks", Icon: Workflow },
  { href: "/webhooks", title: "Webhooks", text: "Push decisions to your automations", Icon: Webhook },
];

export function IntegrationsOverview() {
  const { isLocal } = useMode();
  const test = useMutation({ mutationFn: probe });
  const activity = useQuery({
    queryKey: ["events", "integration-recent", isLocal],
    // Local mode has no API keys, so show all recent activity instead.
    queryFn: () => api.events(isLocal ? { limit: 15 } : { actor_type: "api_key", limit: 15 }),
  });
  const cards = INTEGRATION_CARDS.filter((c) => !(isLocal && c.href === "/integrations/api-keys"));
  const r = test.data;

  return (
    <PageContainer>
      <PageHeader
        title="Integrations"
        icon={<Blocks className="size-4" />}
        description="Connect n8n, Claude Code, Codex, OpenCode and your own scripts to this workspace."
      />

      <div className="mt-6 grid gap-4 lg:grid-cols-[1fr_1.2fr]">
        <div className="rounded-xl border bg-card p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold">Connection</h2>
              <p className="text-sm text-muted-foreground">
                Checks <InlineCode>/health</InlineCode> and your session through the dashboard proxy.
              </p>
            </div>
            <Button size="sm" onClick={() => test.mutate()} disabled={test.isPending}>
              <PlugZap /> {test.isPending ? "Testing…" : "Test connection"}
            </Button>
          </div>
          {r && (
            <div
              role="status"
              className={cn(
                "mt-4 grid gap-2 rounded-lg border p-3 text-sm",
                r.ok ? "border-emerald-500/30 bg-emerald-500/8" : "border-destructive/30 bg-destructive/8",
              )}
            >
              <div className="flex items-center gap-2 font-medium">
                {r.ok ? (
                  <CheckCircle2 className="size-4 text-emerald-600" />
                ) : (
                  <XCircle className="size-4 text-destructive" />
                )}
                {r.ok ? "Connected" : "Connection failed"}
                <span className="ml-auto font-mono text-xs text-muted-foreground">{r.ms} ms</span>
              </div>
              {r.ok ? (
                <dl className="grid grid-cols-[120px_1fr] gap-y-1 text-xs">
                  <dt className="text-muted-foreground">API status</dt>
                  <dd className="font-mono">
                    {r.health?.status}
                    {r.health?.version ? ` · ${r.health.version}` : ""}
                  </dd>
                  <dt className="text-muted-foreground">Workspace</dt>
                  <dd>{r.session?.workspace.name}</dd>
                  <dt className="text-muted-foreground">Signed in as</dt>
                  <dd>
                    {r.session?.principal.name} ({r.session?.principal.type})
                  </dd>
                  <dt className="text-muted-foreground">Agent endpoint</dt>
                  <dd className="font-mono">{API_URL}</dd>
                </dl>
              ) : (
                <p className="text-xs text-muted-foreground">{r.error}</p>
              )}
            </div>
          )}
          <div className="mt-4 grid gap-2 sm:grid-cols-2">
            {cards.map(({ href, title, text, Icon }) => (
              <Link
                key={href}
                href={href}
                className="group flex items-start gap-3 rounded-lg border p-3 transition-colors hover:bg-muted/50"
              >
                <Icon className="mt-0.5 size-4 text-brand" />
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{title}</div>
                  <div className="text-xs text-muted-foreground">{text}</div>
                </div>
                <ArrowRight className="size-3.5 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
              </Link>
            ))}
          </div>
        </div>

        <div className="rounded-xl border bg-card p-4">
          <div className="mb-4 flex items-center justify-between">
            <div>
              <h2 className="text-sm font-semibold">{isLocal ? "Recent activity" : "Recent integration activity"}</h2>
              <p className="text-sm text-muted-foreground">
                {isLocal ? "Latest events from agents and the dashboard." : "Events triggered with API keys."}
              </p>
            </div>
            <Button asChild size="sm" variant="ghost">
              <Link href={isLocal ? "/activity" : "/activity?actor_type=api_key"}>View all</Link>
            </Button>
          </div>
          {activity.isLoading ? (
            <Skeleton className="h-48" />
          ) : activity.error ? (
            <ErrorState error={activity.error} onRetry={() => activity.refetch()} />
          ) : (activity.data?.data ?? []).length === 0 ? (
            <EmptyState
              icon={<Plug />}
              title="No agent activity yet"
              description={
                isLocal
                  ? "Once an agent connects, its actions show up here."
                  : "Once an agent uses an API key, its actions show up here."
              }
            />
          ) : (
            <EventTimeline events={activity.data!.data} showItem />
          )}
        </div>
      </div>
      {isLocal && <CloudModeCard className="mt-4" />}
    </PageContainer>
  );
}

// ---------------------------------------------------------------- REST API

export function ApiDocsPage() {
  const { key, isLocal } = useApiKeyValue();
  const collections = useCollections();
  const col = collections.data?.[0];
  const colRef = col?.id ?? "col_123";
  // Local mode trusts localhost: no Authorization header at all.
  const authLine = isLocal ? "" : ` \\\n  -H "Authorization: Bearer ${key}"`;
  const json = `-H "Content-Type: application/json"`;

  const examples = [
    {
      title: "Create a collection",
      code: `curl -X POST ${API_URL}/v1/collections${authLine} \\\n  ${json} \\\n  -d '{"name":"Instagram Avatar","description":"Avatar drafts for review","default_view":"gallery"}'`,
    },
    {
      title: "Create a text / JSON item",
      code: `curl -X POST ${API_URL}/v1/collections/${colRef}/items${authLine} \\\n  ${json} \\\n  -d '{"type":"json","title":"Post caption","content":{"caption":"Hello world","hashtags":["ai"]},"source":"script","external_ref":"run-42","metadata":{"model":"gpt-image-1"}}'`,
    },
    {
      title: "Upload an image (multipart)",
      code: `curl -X POST ${API_URL}/v1/collections/${colRef}/items${authLine} \\\n  -F "file=@avatar.png" \\\n  -F 'item={"title":"Avatar v1","source":"n8n","metadata":{"prompt":"studio portrait, soft light"}}'`,
    },
    {
      title: "List items waiting for review",
      code: `curl "${API_URL}/v1/items?status=pending&sort=created_asc"${authLine}`,
    },
    {
      title: "Approve an item",
      code: `curl -X POST ${API_URL}/v1/items/itm_123/actions/approve${authLine} \\\n  ${json} \\\n  -d '{}'`,
    },
    {
      title: "Reject with feedback",
      code: `curl -X POST ${API_URL}/v1/items/itm_123/actions/reject${authLine} \\\n  ${json} \\\n  -d '{"feedback":"Background too busy - make the face larger"}'`,
    },
    {
      title: "Mark completed and patch metadata",
      code: `curl -X PATCH ${API_URL}/v1/items/itm_123${authLine} \\\n  ${json} \\\n  -d '{"status":"completed","metadata_patch":{"published_url":"https://instagram.com/p/abc"}}'`,
    },
  ];

  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="REST API"
        icon={<Code2 className="size-4" />}
        description="Everything the dashboard does is available over a versioned JSON API."
        actions={
          <Button asChild variant="outline" size="sm">
            <a href="/api/openapi.yaml" target="_blank" rel="noopener noreferrer">
              <FileCode2 /> openapi.yaml
            </a>
          </Button>
        }
      />
      <div className="mt-6 grid gap-8">
        <DocSection title="Base URL">
          <CodeBlock code={API_URL} />
          <p className="text-sm text-muted-foreground">
            {API_URL_FROM_ENV ? (
              <>
                Set via <InlineCode>NEXT_PUBLIC_API_URL</InlineCode>.
              </>
            ) : (
              <>
                Default for a local install. Set <InlineCode>NEXT_PUBLIC_API_URL</InlineCode> when building the
                dashboard to show a different address.
              </>
            )}{" "}
            Containers on the same Docker network use <InlineCode>http://api:3333</InlineCode>. The full contract is in{" "}
            <a
              href="/api/openapi.yaml"
              className="text-brand hover:underline"
              target="_blank"
              rel="noopener noreferrer"
            >
              openapi.yaml
            </a>
            .
          </p>
        </DocSection>
        <DocSection
          title="Authentication"
          description={
            <>
              {isLocal ? (
                <>No credentials are needed from this machine in local mode. </>
              ) : (
                <>
                  Send <InlineCode>Authorization: Bearer stk_…</InlineCode>.{" "}
                </>
              )}
              Errors look like <InlineCode>{`{"error":{"code","message","fields"}}`}</InlineCode>. Unsafe requests
              accept an <InlineCode>Idempotency-Key</InlineCode> header for safe retries.
            </>
          }
        >
          <KeySourceNote />
        </DocSection>
        <DocSection
          title="Examples"
          description={
            col ? (
              <>Using your collection “{col.name}”. A collection name works in place of its ID when creating items.</>
            ) : (
              "Collection IDs can be replaced by the exact collection name when creating items."
            )
          }
        >
          {examples.map((e) => (
            <CodeBlock key={e.title} title={e.title} code={e.code} language="bash" />
          ))}
        </DocSection>
        {isLocal && <CloudModeCard />}
      </div>
    </PageContainer>
  );
}

// ---------------------------------------------------------------- MCP

export function McpPage() {
  const { key, isLocal } = useApiKeyValue();
  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="MCP server"
        icon={<Plug className="size-4" />}
        description="Let any MCP client — Claude Code, Codex, OpenCode, Cursor, custom agents — read and review your workspace."
      />
      <div className="mt-6 grid gap-8">
        <KeySourceNote />
        <div className="grid gap-4 md:grid-cols-2">
          <div className="grid content-start gap-3 rounded-xl border bg-card p-4">
            <div>
              <div className="text-xs font-medium tracking-wide text-brand uppercase">Recommended</div>
              <h2 className="text-base font-semibold">Streamable HTTP</h2>
              <p className="text-sm text-muted-foreground">
                Stateless endpoint on the API server. Works from anywhere that can reach it.
              </p>
            </div>
            <CodeBlock title="URL" code={MCP_URL} />
            {isLocal ? (
              <p className="text-sm text-muted-foreground">No headers needed in local mode.</p>
            ) : (
              <CodeBlock title="Header" code={`Authorization: Bearer ${key}`} />
            )}
          </div>
          <div className="grid content-start gap-3 rounded-xl border bg-card p-4">
            <div>
              <div className="text-xs font-medium tracking-wide text-muted-foreground uppercase">Alternative</div>
              <h2 className="text-base font-semibold">STDIO via Docker</h2>
              <p className="text-sm text-muted-foreground">
                Runs <InlineCode>mcp-stdio</InlineCode> inside the API container
                {isLocal ? "." : "; the key is passed through the environment."}
              </p>
            </div>
            <CodeBlock
              title="Command"
              language="bash"
              code={
                isLocal
                  ? "docker exec -i staqly-api mcp-stdio"
                  : `STAQLY_API_KEY=${key} \\\n  docker exec -i -e STAQLY_API_KEY staqly-api mcp-stdio`
              }
            />
          </div>
        </div>
        <DocSection
          title="Tools"
          description={
            isLocal
              ? "All tools are available locally. In cloud mode they respect the scopes of the API key."
              : "The tools respect the scopes of the API key you connect with."
          }
        >
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-56">Tool</TableHead>
                  <TableHead>What it does</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {MCP_TOOLS.map((t) => (
                  <TableRow key={t.name}>
                    <TableCell className="font-mono text-xs">{t.name}</TableCell>
                    <TableCell className="text-muted-foreground">{t.description}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </DocSection>
        <DocSection title="Client guides">
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline" size="sm">
              <Link href="/integrations/claude-code">
                <Terminal /> Claude Code
              </Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link href="/integrations/codex">
                <FileCode2 /> Codex
              </Link>
            </Button>
            <Button asChild variant="outline" size="sm">
              <Link href="/integrations/opencode">
                <SquareTerminal /> OpenCode
              </Link>
            </Button>
          </div>
        </DocSection>
        {isLocal && <CloudModeCard />}
      </div>
    </PageContainer>
  );
}

// ---------------------------------------------------------------- Claude Code

const CLAUDE_PROMPTS = [
  "Create a collection called Instagram Avatar",
  "Show me all pending images",
  "Find the images I approved yesterday",
  "Reject itm_… with feedback: the lighting is too dark, regenerate with a warmer palette",
  "Save this summary as a text item in the Research collection",
];

export function ClaudeCodePage() {
  const { key, isLocal } = useApiKeyValue();
  const server = isLocal
    ? { type: "http", url: MCP_URL }
    : { type: "http", url: MCP_URL, headers: { Authorization: "Bearer ${STAQLY_API_KEY}" } };
  const mcpJson = JSON.stringify({ mcpServers: { staqly: server } }, null, 2);
  const httpCmd = isLocal
    ? `claude mcp add --transport http staqly ${MCP_URL}`
    : `claude mcp add --transport http staqly ${MCP_URL} --header "Authorization: Bearer ${key}"`;
  const stdioCmd = isLocal
    ? "claude mcp add staqly -- docker exec -i staqly-api mcp-stdio"
    : `claude mcp add staqly -e STAQLY_API_KEY=${key} -- docker exec -i -e STAQLY_API_KEY staqly-api mcp-stdio`;
  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="Claude Code"
        icon={<Terminal className="size-4" />}
        description="Give Claude Code read/write access to your workspace through MCP."
      />
      <div className="mt-6 grid gap-8">
        <KeySourceNote />
        <DocSection step={1} title="Add the MCP server (HTTP)">
          <CodeBlock language="bash" code={httpCmd} />
          <p className="text-sm text-muted-foreground">
            Run <InlineCode>claude mcp list</InlineCode> to confirm it connected, or <InlineCode>/mcp</InlineCode>{" "}
            inside a session.
          </p>
        </DocSection>
        <DocSection step={2} title="…or use STDIO through Docker">
          <CodeBlock language="bash" code={stdioCmd} />
        </DocSection>
        <DocSection
          step={3}
          title="Share it with your project"
          description={
            isLocal ? (
              <>
                Commit a <InlineCode>.mcp.json</InlineCode> so everyone on the project gets the server.
              </>
            ) : (
              <>
                Commit a <InlineCode>.mcp.json</InlineCode> and keep the key out of git — Claude Code expands{" "}
                <InlineCode>{"${STAQLY_API_KEY}"}</InlineCode> from your environment.
              </>
            )
          }
        >
          <CodeBlock title=".mcp.json" language="json" code={mcpJson} />
          {!isLocal && <CodeBlock language="bash" code={`export STAQLY_API_KEY=${key}`} />}
        </DocSection>
        <DocSection step={4} title="Try it">
          <ul className="grid gap-2">
            {CLAUDE_PROMPTS.map((p) => (
              <li key={p} className="rounded-lg border bg-card px-3 py-2 text-sm">
                “{p}”
              </li>
            ))}
          </ul>
        </DocSection>
        {isLocal && <CloudModeCard />}
      </div>
    </PageContainer>
  );
}

// ---------------------------------------------------------------- Codex

export function CodexPage() {
  const { key, isLocal } = useApiKeyValue();
  let step = 0;
  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="Codex"
        icon={<FileCode2 className="size-4" />}
        description="Connect the OpenAI Codex CLI to staq.ly over MCP."
      />
      <div className="mt-6 grid gap-8">
        <KeySourceNote />
        {!isLocal && (
          <DocSection
            step={++step}
            title="Export your key"
            description="Codex reads the bearer token from an environment variable."
          >
            <CodeBlock language="bash" code={`export STAQLY_API_KEY=${key}`} />
          </DocSection>
        )}
        <DocSection step={++step} title="Add the server">
          <CodeBlock
            language="bash"
            code={
              isLocal
                ? `codex mcp add staqly --url ${MCP_URL}`
                : `codex mcp add staqly --url ${MCP_URL} --bearer-token-env-var STAQLY_API_KEY`
            }
          />
          <p className="text-sm text-muted-foreground">
            Equivalent <InlineCode>~/.codex/config.toml</InlineCode>:
          </p>
          <CodeBlock
            title="~/.codex/config.toml"
            language="toml"
            code={
              isLocal
                ? `[mcp_servers.staqly]\nurl = "${MCP_URL}"`
                : `[mcp_servers.staqly]\nurl = "${MCP_URL}"\nbearer_token_env_var = "STAQLY_API_KEY"`
            }
          />
        </DocSection>
        <DocSection step={++step} title="STDIO alternative">
          <CodeBlock
            language="bash"
            code={
              isLocal
                ? "codex mcp add staqly -- docker exec -i staqly-api mcp-stdio"
                : `codex mcp add staqly --env STAQLY_API_KEY=${key} -- docker exec -i -e STAQLY_API_KEY staqly-api mcp-stdio`
            }
          />
        </DocSection>
        <DocSection step={++step} title="Try it">
          <ul className="grid gap-2">
            {CLAUDE_PROMPTS.slice(0, 4).map((p) => (
              <li key={p} className="rounded-lg border bg-card px-3 py-2 text-sm">
                “{p}”
              </li>
            ))}
          </ul>
        </DocSection>
        {isLocal && <CloudModeCard />}
      </div>
    </PageContainer>
  );
}

// ---------------------------------------------------------------- OpenCode

export function openCodeConfig(isLocal: boolean) {
  const staqly = isLocal
    ? { type: "remote", url: MCP_URL, enabled: true }
    : { type: "remote", url: MCP_URL, headers: { Authorization: "Bearer {env:STAQLY_API_KEY}" }, enabled: true };
  return JSON.stringify({ $schema: "https://opencode.ai/config.json", mcp: { staqly } }, null, 2);
}

export function openCodeStdioConfig(isLocal: boolean) {
  const staqly = isLocal
    ? { type: "local", command: ["docker", "exec", "-i", "staqly-api", "mcp-stdio"], enabled: true }
    : {
        type: "local",
        command: ["docker", "exec", "-i", "-e", "STAQLY_API_KEY", "staqly-api", "mcp-stdio"],
        environment: { STAQLY_API_KEY: "{env:STAQLY_API_KEY}" },
        enabled: true,
      };
  return JSON.stringify({ $schema: "https://opencode.ai/config.json", mcp: { staqly } }, null, 2);
}

export function OpenCodePage() {
  const { key, isLocal } = useApiKeyValue();
  let step = 0;
  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="OpenCode"
        icon={<SquareTerminal className="size-4" />}
        description="Connect the OpenCode terminal agent to staq.ly over MCP."
      />
      <div className="mt-6 grid gap-8">
        <KeySourceNote />
        {!isLocal && (
          <DocSection
            step={++step}
            title="Export your key"
            description={
              <>
                OpenCode substitutes <InlineCode>{"{env:STAQLY_API_KEY}"}</InlineCode> from your environment, so the key
                stays out of the config file.
              </>
            }
          >
            <CodeBlock language="bash" code={`export STAQLY_API_KEY=${key}`} />
          </DocSection>
        )}
        <DocSection
          step={++step}
          title="Add the server to opencode.json"
          description={
            <>
              Put this in <InlineCode>opencode.json</InlineCode> at the root of your project (or merge the{" "}
              <InlineCode>mcp</InlineCode> block into an existing one).
            </>
          }
        >
          <CodeBlock title="opencode.json" language="json" code={openCodeConfig(isLocal)} />
        </DocSection>
        <DocSection step={++step} title="Check the connection">
          <CodeBlock language="bash" code="opencode mcp list" />
          <p className="text-sm text-muted-foreground">
            You should see <InlineCode>✓ staqly connected</InlineCode>. Then start <InlineCode>opencode</InlineCode> in
            the project and ask for something below.
          </p>
        </DocSection>
        <DocSection
          step={++step}
          title="…or use STDIO through Docker"
          description="Runs mcp-stdio inside the API container instead of connecting over HTTP."
        >
          <CodeBlock title="opencode.json" language="json" code={openCodeStdioConfig(isLocal)} />
        </DocSection>
        <DocSection step={++step} title="Try it">
          <ul className="grid gap-2">
            {[
              ...CLAUDE_PROMPTS.slice(0, 3),
              "Watch r/midjourney and drop new posts into Inspiration",
              "Auto-approve images with metadata.score ≥ 0.9",
            ].map((p) => (
              <li key={p} className="rounded-lg border bg-card px-3 py-2 text-sm">
                “{p}”
              </li>
            ))}
          </ul>
        </DocSection>
        {isLocal && <CloudModeCard />}
      </div>
    </PageContainer>
  );
}

// ---------------------------------------------------------------- n8n

const VERIFY_SNIPPET = `// n8n Code node (or any Node.js service).
// Enable "Raw Body" on the Webhook node so the exact bytes are available.
const crypto = require("crypto");

function verifyStaqly(secret, headers, rawBody) {
  const ts = headers["x-staqly-timestamp"];
  const sig = headers["x-staqly-signature"] || "";
  // Reject stale deliveries (5 minute tolerance) to prevent replays.
  if (Math.abs(Date.now() / 1000 - Number(ts)) > 300) return false;
  const expected =
    "sha256=" + crypto.createHmac("sha256", secret).update(ts + "." + rawBody).digest("hex");
  return sig.length === expected.length &&
    crypto.timingSafeEqual(Buffer.from(sig), Buffer.from(expected));
}`;

const PAYLOAD_EXAMPLE = `{
  "id": "evt_…",
  "type": "item.rejected",
  "created_at": "2026-01-01T12:00:00Z",
  "workspace_id": "ws_…",
  "delivery_id": "dlv_…",
  "attempt": 1,
  "actor": { "type": "session", "id": "…", "name": "Dashboard" },
  "data": {
    "item": { "id": "itm_…", "title": "Avatar v1", "status": "rejected", "feedback": "…" },
    "collection": { "id": "col_…", "name": "Instagram Avatar" },
    "action": "reject",
    "feedback": "Make the face larger",
    "previous_status": "pending"
  }
}`;

export function N8nPage() {
  const { key, isLocal } = useApiKeyValue();
  const rows = [
    {
      where: "n8n on the host (npx / desktop app)",
      api: "http://localhost:3333",
      hook: "http://host.docker.internal:5678/webhook/…",
      note: "staq.ly calls the webhook from inside its container, so it has to go through the host.",
    },
    {
      where: "n8n on the staq.ly Docker network",
      api: "http://api:3333",
      hook: "http://n8n:5678/webhook/…",
      note: "e.g. the example in integrations/n8n/compose.yml, which joins the staqly_default network.",
    },
    {
      where: "n8n anywhere else",
      api: "Any address of the staq.ly API that n8n can reach",
      hook: "Any address of n8n that the staq.ly API can reach",
      note: "Bring your own n8n — staq.ly does not bundle it. Each side just needs to reach the other.",
    },
  ];
  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="n8n"
        icon={<Workflow className="size-4" />}
        description="Push generated content into staq.ly and react to review decisions with signed webhooks."
      />
      <div className="mt-6 grid gap-8">
        <KeySourceNote />
        <DocSection
          title="Networking"
          description="Bring your own n8n — which addresses to use depends on where it runs. To try it next to staq.ly, start the example: docker compose -f integrations/n8n/compose.yml up -d"
        >
          <div className="overflow-x-auto rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>n8n runs…</TableHead>
                  <TableHead>staq.ly API URL (HTTP Request node)</TableHead>
                  <TableHead>Webhook URL (in staq.ly)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((r) => (
                  <TableRow key={r.where}>
                    <TableCell className="align-top whitespace-normal">
                      <div className="font-medium">{r.where}</div>
                      <div className="text-xs text-muted-foreground">{r.note}</div>
                    </TableCell>
                    <TableCell className="align-top font-mono text-xs whitespace-normal">{r.api}</TableCell>
                    <TableCell className="align-top font-mono text-xs whitespace-normal">{r.hook}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </DocSection>
        <DocSection
          step={1}
          title="Call the API with the HTTP Request node"
          description={
            isLocal ? (
              <>
                In local mode HTTP Request nodes need <strong>no authentication</strong> — just point them at the API
                URL above.
              </>
            ) : (
              <>
                Create a <strong>Header Auth</strong> credential: name <InlineCode>Authorization</InlineCode>, value{" "}
                <InlineCode>Bearer {key}</InlineCode>. Use an API key with the “Automation (n8n)” preset.
              </>
            )
          }
        >
          {!isLocal && <CodeBlock title="Header Auth credential" code={`Name:  Authorization\nValue: Bearer ${key}`} />}
          <p className="text-sm text-muted-foreground">
            To upload an image, send <strong>Form-Data</strong> to{" "}
            <InlineCode>POST /v1/collections/&lt;id or name&gt;/items</InlineCode> with a binary{" "}
            <InlineCode>file</InlineCode> field and an optional <InlineCode>item</InlineCode> field containing JSON
            (title, metadata, external_ref). See the{" "}
            <Link href="/integrations/api" className="text-brand hover:underline">
              REST API examples
            </Link>
            .
          </p>
        </DocSection>
        <DocSection
          step={2}
          title="Receive decisions with a webhook"
          description={
            <>
              Add a Webhook trigger in n8n, then register its URL on the{" "}
              <Link href="/webhooks" className="text-brand hover:underline">
                Webhooks
              </Link>{" "}
              page subscribed to e.g. <InlineCode>item.approved</InlineCode>, <InlineCode>item.rejected</InlineCode>,{" "}
              <InlineCode>item.regenerate_requested</InlineCode>.
            </>
          }
        >
          <CodeBlock title="Payload" language="json" code={PAYLOAD_EXAMPLE} />
        </DocSection>
        <DocSection
          step={3}
          title="Verify the signature"
          description={
            <>
              <InlineCode>X-Staqly-Signature</InlineCode> ={" "}
              <InlineCode>
                &quot;sha256=&quot; + hex(HMAC-SHA256(secret, X-Staqly-Timestamp + &quot;.&quot; + rawBody))
              </InlineCode>
              . The secret is shown on the webhook.
            </>
          }
        >
          <CodeBlock title="verify.js" language="js" code={VERIFY_SNIPPET} />
        </DocSection>
        <DocSection
          step={4}
          title="Import the demo workflow"
          description={
            <>
              A ready-made Instagram Avatar review loop lives in the repository at{" "}
              <InlineCode>integrations/n8n/instagram-avatar-demo.json</InlineCode>. Import it in n8n via{" "}
              <em>Workflows → Import from file</em>.
            </>
          }
        />
      </div>
    </PageContainer>
  );
}
