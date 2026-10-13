"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, KeyRound, MoreHorizontal, Plus, RotateCw, ShieldOff } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState, ErrorState } from "@/components/common/error-state";
import { PageContainer, PageHeader } from "@/components/common/page-header";
import { SecretField } from "@/components/common/secret-field";
import { ALL_SCOPES, api, type APIKey, type Scope } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { formatDateTime, timeAgo } from "@/lib/format";
import { qk, useMode } from "@/lib/queries";
import { CloudModeCard } from "./shared";
import { rememberApiKey } from "@/lib/session-key";
import { cn } from "@/lib/utils";

export const SCOPE_INFO: Record<Scope, string> = {
  "collections:read": "List and read collections",
  "collections:write": "Create, rename and archive collections",
  "items:read": "List, search and download items",
  "items:write": "Create, edit and upload items",
  "items:review": "Approve and reject items",
  "actions:execute": "Run custom collection actions",
  "webhooks:manage": "Manage webhooks and deliveries",
  "rules:manage": "Create and edit approval rules",
  "watchers:manage": "Create, pause and run watchers",
  "channels:manage": "Manage notification channels (they hold chat tokens)",
  admin: "Manage API keys, sessions and workspace",
};

export const SCOPE_PRESETS: { id: string; label: string; description: string; scopes: Scope[] }[] = [
  {
    id: "automation",
    label: "Automation (n8n)",
    description: "Create collections & items, run actions",
    scopes: ["collections:read", "collections:write", "items:read", "items:write", "actions:execute"],
  },
  {
    id: "readonly",
    label: "Read-only agent",
    description: "Look things up, change nothing",
    scopes: ["collections:read", "items:read"],
  },
  {
    id: "reviewer",
    label: "Reviewer agent",
    description: "Read, approve/reject and run actions",
    scopes: ["collections:read", "items:read", "items:review", "actions:execute"],
  },
  {
    id: "full",
    label: "Full access",
    description: "Every scope, including admin",
    scopes: [...ALL_SCOPES],
  },
];

function keyState(k: APIKey): { label: string; cls: string } {
  if (k.revoked_at) return { label: "Revoked", cls: "bg-muted text-muted-foreground" };
  if (k.expires_at && new Date(k.expires_at).getTime() < Date.now())
    return { label: "Expired", cls: "bg-orange-500/12 text-orange-700 dark:text-orange-300" };
  return { label: "Active", cls: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300" };
}

function sameSet(a: string[], b: string[]) {
  return a.length === b.length && a.every((x) => b.includes(x));
}

export function NewKeyDialog({
  apiKey,
  onOpenChange,
  title = "Your new API key",
}: {
  apiKey: APIKey | null;
  onOpenChange: (o: boolean) => void;
  title?: string;
}) {
  return (
    <Dialog open={!!apiKey} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            Copy it now — for your security it is <strong>shown only once</strong>. staq.ly stores only a hash.
          </DialogDescription>
        </DialogHeader>
        {apiKey?.key && (
          <div className="grid gap-3">
            <SecretField value={apiKey.key} label="New API key" />
            <div className="flex flex-wrap gap-1">
              {apiKey.scopes.map((s) => (
                <span key={s} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">
                  {s}
                </span>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              The integration pages in this tab are pre-filled with this key until you reload.
            </p>
          </div>
        )}
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>I&apos;ve copied it</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function CreateKeyDialog({
  open,
  onOpenChange,
  availableScopes,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  availableScopes: Scope[];
  onCreated: (k: APIKey) => void;
}) {
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<Scope[]>(SCOPE_PRESETS[0].scopes);
  const [expires, setExpires] = useState("");
  const [nameError, setNameError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.createApiKey({ name: name.trim(), scopes, ...(expires ? { expires_at: expires } : {}) }),
    onSuccess: (k) => {
      void qc.invalidateQueries({ queryKey: qk.apiKeys });
      if (k.key) rememberApiKey({ name: k.name, key: k.key });
      setName("");
      setExpires("");
      setScopes(SCOPE_PRESETS[0].scopes);
      onOpenChange(false);
      onCreated(k);
    },
    onError: (e) => toastApiError(e),
  });

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setNameError("Give the key a name so you know where it is used");
      return;
    }
    if (!scopes.length) {
      toast.error("Select at least one scope");
      return;
    }
    create.mutate();
  }

  const scopeList = availableScopes.length ? availableScopes : ALL_SCOPES;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <DialogHeader>
            <DialogTitle>Create API key</DialogTitle>
            <DialogDescription>
              Give each agent or automation its own key with only the scopes it needs.
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="key-name">Name</Label>
            <Input
              id="key-name"
              // oxlint-disable-next-line jsx-a11y/no-autofocus -- first field of a modal dialog (WAI-ARIA dialog pattern)
              autoFocus
              value={name}
              placeholder="n8n production"
              onChange={(e) => {
                setName(e.target.value);
                setNameError(null);
              }}
              aria-invalid={!!nameError}
            />
            {nameError && <p className="text-sm text-destructive">{nameError}</p>}
          </div>
          <div className="grid gap-2">
            <Label>Preset</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              {SCOPE_PRESETS.map((p) => {
                const active = sameSet(p.scopes, scopes);
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => setScopes(p.scopes.filter((s) => scopeList.includes(s)))}
                    aria-pressed={active}
                    className={cn(
                      "rounded-lg border p-2.5 text-left transition-colors hover:bg-muted/50",
                      active && "border-brand bg-brand-soft hover:bg-brand-soft",
                    )}
                  >
                    <div className="text-sm font-medium">{p.label}</div>
                    <div className="text-xs text-muted-foreground">{p.description}</div>
                  </button>
                );
              })}
            </div>
          </div>
          <fieldset className="grid gap-2">
            <legend className="mb-2 text-sm font-medium">Scopes</legend>
            <div className="grid gap-1.5 sm:grid-cols-2">
              {scopeList.map((s) => (
                <label key={s} className="flex items-start gap-2 rounded-md p-1 text-sm hover:bg-muted/40">
                  <Checkbox
                    checked={scopes.includes(s)}
                    onCheckedChange={(v) => setScopes((cur) => (v ? [...cur, s] : cur.filter((x) => x !== s)))}
                    aria-label={s}
                    className="mt-0.5"
                  />
                  <span>
                    <span className="font-mono text-xs">{s}</span>
                    <span className="block text-xs text-muted-foreground">{SCOPE_INFO[s]}</span>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
          {scopes.includes("admin") && (
            <div className="flex gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 p-2.5 text-xs">
              <AlertTriangle className="size-4 shrink-0 text-amber-600" />
              Admin keys can create and revoke other keys and manage sessions. Only use them for trusted tooling.
            </div>
          )}
          <div className="grid gap-2">
            <Label htmlFor="key-expires">
              Expires <span className="font-normal text-muted-foreground">(optional)</span>
            </Label>
            <Input
              id="key-expires"
              type="date"
              value={expires}
              onChange={(e) => setExpires(e.target.value)}
              className="w-48"
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? "Creating…" : "Create key"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ApiKeysManager() {
  const { isLocal } = useMode();
  if (isLocal) {
    return (
      <PageContainer className="max-w-4xl">
        <PageHeader
          title="API keys"
          icon={<KeyRound className="size-4" />}
          description="Not needed in local mode — agents on this machine connect without credentials."
        />
        <CloudModeCard className="mt-6" />
      </PageContainer>
    );
  }
  return <CloudApiKeysManager />;
}

function CloudApiKeysManager() {
  const params = useSearchParams();
  const qc = useQueryClient();
  const keys = useQuery({ queryKey: qk.apiKeys, queryFn: api.apiKeys });
  const [createOpen, setCreateOpen] = useState(params.get("new") === "1");
  const [revealed, setRevealed] = useState<{ key: APIKey; title: string } | null>(null);
  const [confirm, setConfirm] = useState<{ key: APIKey; kind: "revoke" | "rotate" } | null>(null);

  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeApiKey(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.apiKeys });
      toast.success("API key revoked");
    },
    onError: (e) => toastApiError(e),
  });
  const rotate = useMutation({
    mutationFn: (id: string) => api.rotateApiKey(id),
    onSuccess: (k) => {
      void qc.invalidateQueries({ queryKey: qk.apiKeys });
      if (k.key) rememberApiKey({ name: k.name, key: k.key });
      setRevealed({ key: k, title: "Rotated API key" });
    },
    onError: (e) => toastApiError(e),
  });

  const list = (keys.data?.data ?? []).toSorted(
    (a, b) => Number(!!a.revoked_at) - Number(!!b.revoked_at) || b.created_at.localeCompare(a.created_at),
  );

  return (
    <PageContainer>
      <PageHeader
        title="API keys"
        icon={<KeyRound className="size-4" />}
        description="Bearer tokens for agents and automations. Send them as Authorization: Bearer stk_…"
        actions={
          <Button onClick={() => setCreateOpen(true)}>
            <Plus /> Create API key
          </Button>
        }
      />
      <div className="mt-6">
        {keys.isLoading ? (
          <Skeleton className="h-48" />
        ) : keys.error ? (
          <ErrorState error={keys.error} onRetry={() => keys.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            icon={<KeyRound />}
            title="No API keys"
            description="Create a key for each agent or automation."
          />
        ) : (
          <div className="overflow-hidden rounded-xl border bg-card">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>Name</TableHead>
                  <TableHead>Key</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead className="hidden md:table-cell">Last used</TableHead>
                  <TableHead className="hidden lg:table-cell">Created</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.map((k) => {
                  const st = keyState(k);
                  return (
                    <TableRow key={k.id} className={cn(k.revoked_at && "opacity-60")} data-testid="api-key-row">
                      <TableCell className="font-medium">{k.name}</TableCell>
                      <TableCell className="font-mono text-xs">{k.prefix}…</TableCell>
                      <TableCell className="max-w-72">
                        <div className="flex flex-wrap gap-1">
                          {k.scopes.map((s) => (
                            <span
                              key={s}
                              className="rounded bg-muted px-1 py-0.5 font-mono text-[10.5px]"
                              title={SCOPE_INFO[s]}
                            >
                              {s}
                            </span>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell
                        className="hidden text-muted-foreground md:table-cell"
                        title={formatDateTime(k.last_used_at)}
                      >
                        {k.last_used_at ? timeAgo(k.last_used_at) : "Never"}
                      </TableCell>
                      <TableCell
                        className="hidden text-muted-foreground lg:table-cell"
                        title={formatDateTime(k.created_at)}
                      >
                        {timeAgo(k.created_at)}
                        {k.expires_at && (
                          <span className="block text-[11px]">expires {formatDateTime(k.expires_at)}</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <span className={cn("rounded-md px-1.5 py-0.5 text-[11px] font-medium", st.cls)}>
                          {st.label}
                        </span>
                      </TableCell>
                      <TableCell>
                        {!k.revoked_at && (
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="icon-sm" aria-label={`Options for ${k.name}`}>
                                <MoreHorizontal />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem onSelect={() => setConfirm({ key: k, kind: "rotate" })}>
                                <RotateCw /> Rotate
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                variant="destructive"
                                onSelect={() => setConfirm({ key: k, kind: "revoke" })}
                              >
                                <ShieldOff /> Revoke
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </div>

      <CreateKeyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        availableScopes={keys.data?.available_scopes ?? []}
        onCreated={(k) => setRevealed({ key: k, title: "Your new API key" })}
      />
      <NewKeyDialog
        apiKey={revealed?.key ?? null}
        title={revealed?.title}
        onOpenChange={(o) => !o && setRevealed(null)}
      />
      <AlertDialog open={!!confirm} onOpenChange={(o) => !o && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirm?.kind === "rotate" ? "Rotate" : "Revoke"} “{confirm?.key.name}”?
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirm?.kind === "rotate"
                ? "The current key stops working immediately and a replacement with the same name and scopes is issued. Update every integration that uses it."
                : "Any agent or automation using this key will immediately get 401 errors. This cannot be undone."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                if (!confirm) return;
                if (confirm.kind === "rotate") rotate.mutate(confirm.key.id);
                else revoke.mutate(confirm.key.id);
              }}
            >
              {confirm?.kind === "rotate" ? "Rotate key" : "Revoke key"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageContainer>
  );
}
