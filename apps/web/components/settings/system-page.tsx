"use client";

import { useQuery } from "@tanstack/react-query";
import { BookOpen, CircleCheck, CircleX, HeartPulse, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { InlineCode } from "@/components/common/code-block";
import { ErrorState } from "@/components/common/error-state";
import { PageContainer, PageHeader } from "@/components/common/page-header";
import { api, type Readiness } from "@/lib/api";
import { PUBLIC_API_URL } from "@/lib/config";
import { qk, useMode } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { SettingsCard } from "./settings-card";
import { SettingsNav } from "./settings-nav";

const CHECKS: { key: keyof Readiness["checks"]; label: string; hint: string }[] = [
  { key: "database", label: "Database", hint: "SQLite answers a ping" },
  { key: "migrations", label: "Migrations", hint: "Every schema migration is applied" },
  { key: "workers", label: "Background workers", hint: "Webhooks, watchers and notifications are running" },
];

/** `not_started` is not a failure (processes without workers, e.g. tests). */
export function checkOk(key: keyof Readiness["checks"], value?: string) {
  if (!value) return null;
  if (key === "workers" && value === "not_started") return true;
  return value === "ok";
}

function ReadinessCard() {
  const ready = useQuery({ queryKey: qk.readiness, queryFn: api.readyz, refetchInterval: 30_000, staleTime: 0 });
  const health = useQuery({ queryKey: qk.health, queryFn: api.health, staleTime: 60_000 });
  const ok = ready.data?.status === "ok";
  return (
    <SettingsCard
      title="Health"
      icon={<HeartPulse />}
      description="Live readiness of the staq.ly API, refreshed every 30 seconds."
      action={
        <Button
          size="sm"
          variant="outline"
          onClick={() => ready.refetch()}
          disabled={ready.isFetching}
          className="pointer-coarse:h-10"
        >
          <RefreshCw className={ready.isFetching ? "animate-spin" : undefined} /> Check now
        </Button>
      }
    >
      {ready.isLoading ? (
        <Skeleton className="h-28" />
      ) : ready.error ? (
        <ErrorState title="API not reachable" error={ready.error} onRetry={() => ready.refetch()} />
      ) : (
        <div className="grid gap-3" data-testid="readiness" data-status={ready.data?.status}>
          <div
            className={cn(
              "flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium",
              ok
                ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
                : "bg-rose-500/10 text-rose-700 dark:text-rose-300",
            )}
          >
            {ok ? <CircleCheck className="size-4" aria-hidden /> : <CircleX className="size-4" aria-hidden />}
            {ok ? "Ready" : "Not ready"}
            {health.data?.version && (
              <span className="ml-auto font-mono text-xs font-normal text-muted-foreground">
                {/^\d/.test(health.data.version) ? `v${health.data.version}` : health.data.version}
              </span>
            )}
          </div>
          <ul className="divide-y rounded-lg border">
            {CHECKS.map((c) => {
              const value = ready.data?.checks?.[c.key];
              const good = checkOk(c.key, value);
              return (
                <li key={c.key} className="flex items-center gap-3 px-3 py-2.5 text-sm">
                  {good === null ? (
                    <span className="size-4 shrink-0 rounded-full border" aria-hidden />
                  ) : good ? (
                    <CircleCheck className="size-4 shrink-0 text-emerald-600" aria-hidden />
                  ) : (
                    <CircleX className="size-4 shrink-0 text-rose-600" aria-hidden />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium">{c.label}</span>
                    <span className="block text-xs text-muted-foreground">{c.hint}</span>
                  </span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">{value ?? "—"}</span>
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </SettingsCard>
  );
}

export function SystemPage() {
  const { isLocal } = useMode();
  return (
    <PageContainer className="max-w-3xl">
      <PageHeader title="Settings" description="Workspace, notifications, data and system status." />
      <SettingsNav className="mt-4" />
      <div className="mt-6 grid gap-4">
        <ReadinessCard />
        <SettingsCard title="Monitoring" icon={<BookOpen />} description="For uptime checks, Prometheus and alerts.">
          <ul className="grid gap-2 text-sm">
            <li>
              <InlineCode>GET {PUBLIC_API_URL}/livez</InlineCode>{" "}
              <span className="text-muted-foreground">— liveness, never touches the database</span>
            </li>
            <li>
              <InlineCode>GET {PUBLIC_API_URL}/readyz</InlineCode>{" "}
              <span className="text-muted-foreground">— readiness (what this page shows)</span>
            </li>
            <li>
              <InlineCode>GET {PUBLIC_API_URL}/metrics</InlineCode>{" "}
              <span className="text-muted-foreground">
                — Prometheus; {isLocal ? "allowed for curl on this machine, otherwise needs" : "needs"}{" "}
                <InlineCode>METRICS_TOKEN</InlineCode>
              </span>
            </li>
            <li className="text-muted-foreground">
              Request IDs, rate limits, Sentry and Grafana tips: <InlineCode>docs/observability.md</InlineCode>
            </li>
          </ul>
        </SettingsCard>
      </div>
    </PageContainer>
  );
}
