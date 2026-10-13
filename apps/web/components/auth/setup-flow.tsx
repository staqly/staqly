"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  CheckCheck,
  CreditCard,
  Download,
  KeyRound,
  LifeBuoy,
  Lock,
  MailX,
  Webhook,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorState } from "@/components/common/error-state";
import { SecretField } from "@/components/common/secret-field";
import { PlanBadge } from "@/components/plans/plan-badge";
import { EVERY_PLAN, PlanGrid } from "@/components/plans/plan-cards";
import { api, ApiError, setCsrfToken, type SetupResult } from "@/lib/api";
import { downloadText } from "@/lib/format";
import { planName, toastApiError } from "@/lib/notify";
import { usePlans, useSetupStatus } from "@/lib/queries";
import { rememberApiKey } from "@/lib/session-key";
import { cn } from "@/lib/utils";
import { PublicShell } from "./public-shell";

const schema = z.object({
  name: z.string().trim().max(100, "At most 100 characters"),
  setup_token: z.string(),
});
type Values = z.infer<typeof schema>;

const NAME_INPUT_ID = "workspace-name";

const POINTS = [
  { Icon: Bot, text: "n8n, Claude Code, Codex and scripts drop their output into collections." },
  { Icon: CheckCheck, text: "Approve, reject with feedback or edit — at your desk or on your phone." },
  { Icon: Webhook, text: "Signed webhooks and MCP tell your agents what you decided." },
];

export function SetupFlow() {
  const status = useSetupStatus();
  const router = useRouter();
  const [result, setResult] = useState<SetupResult | null>(null);
  const local = status.data?.mode === "local";

  // Local mode has no onboarding: the workspace exists and localhost is trusted.
  useEffect(() => {
    if (local) router.replace("/inbox");
  }, [local, router]);

  const signIn = (
    <Button asChild variant="ghost" className="pointer-coarse:h-11">
      <Link href="/login">
        Sign in <ArrowRight />
      </Link>
    </Button>
  );

  if (result) {
    return (
      <PublicShell className="max-w-xl pt-6 sm:pt-10">
        <SecretsStep result={result} />
      </PublicShell>
    );
  }
  if (status.isPending || local) {
    return (
      <PublicShell className="pt-10">
        <Skeleton className="mx-auto h-72 w-full max-w-md rounded-2xl" />
      </PublicShell>
    );
  }
  if (status.error) {
    return (
      <PublicShell className="max-w-md pt-10">
        <ErrorState title="Cannot reach the staq.ly API" error={status.error} onRetry={() => status.refetch()} />
      </PublicShell>
    );
  }
  const tokenRequired = !!status.data.setup_token_required;
  // Older servers did not report signup_open; treat them as open.
  const closed = status.data.signup_open === false && !tokenRequired;
  if (closed) {
    return (
      <PublicShell nav={signIn} className="max-w-md pt-10">
        <SignupClosed />
      </PublicShell>
    );
  }
  return (
    <PublicShell nav={signIn}>
      <CreateWorkspace tokenRequired={tokenRequired} onDone={setResult} />
    </PublicShell>
  );
}

function SignupClosed() {
  return (
    <div className="grid gap-4 rounded-2xl border bg-card p-6 text-center shadow-sm">
      <span className="mx-auto flex size-11 items-center justify-center rounded-xl bg-muted">
        <Lock className="size-5 text-muted-foreground" />
      </span>
      <h1 className="text-xl font-semibold tracking-tight">Sign-ups are closed on this server</h1>
      <p className="text-sm text-muted-foreground">
        Ask the operator for an invite, or sign in to an existing workspace with its recovery key.
      </p>
      <Button asChild size="lg" className="h-11">
        <Link href="/login">
          Sign in to a workspace <ArrowRight />
        </Link>
      </Button>
    </div>
  );
}

function focusForm() {
  const el = document.getElementById(NAME_INPUT_ID);
  el?.scrollIntoView({ behavior: "smooth", block: "center" });
  el?.focus({ preventScroll: true });
}

function CreateWorkspace({ tokenRequired, onDone }: { tokenRequired: boolean; onDone: (r: SetupResult) => void }) {
  const plans = usePlans();

  return (
    <div className="grid gap-14 pt-4 sm:pt-10 lg:gap-20">
      <section className="grid items-center gap-8 lg:grid-cols-[1.1fr_1fr] lg:gap-14">
        <div className="max-w-xl">
          <div className="mb-4 inline-flex flex-wrap items-center gap-x-3 gap-y-1 rounded-full border bg-background/70 px-3 py-1 text-xs font-medium text-muted-foreground shadow-xs backdrop-blur">
            <span className="inline-flex items-center gap-1">
              <CreditCard className="size-3.5 text-brand" /> No card
            </span>
            <span className="inline-flex items-center gap-1">
              <MailX className="size-3.5 text-brand" /> No email
            </span>
            <span>Ready in seconds</span>
          </div>
          <h1 className="text-[2.1rem] leading-[1.08] font-semibold tracking-tight text-balance sm:text-5xl">
            Create your free workspace
          </h1>
          <p className="mt-4 text-base text-muted-foreground sm:text-lg">
            The workspace for everything your AI agents create. Agents store their work; you review, approve and send
            feedback — and they pick it up automatically.
          </p>
          <ul className="mt-6 hidden gap-3 sm:grid">
            {POINTS.map(({ Icon, text }) => (
              <li key={text} className="flex items-start gap-3 text-sm">
                <span className="flex size-7 shrink-0 items-center justify-center rounded-lg border bg-card shadow-xs">
                  <Icon className="size-3.5 text-brand" />
                </span>
                <span className="pt-1 text-muted-foreground">{text}</span>
              </li>
            ))}
          </ul>
        </div>
        <WorkspaceForm tokenRequired={tokenRequired} onDone={onDone} />
      </section>

      <section aria-labelledby="pricing-heading" className="grid gap-6">
        <div className="mx-auto max-w-2xl text-center">
          <div className="text-xs font-semibold tracking-[0.18em] text-brand uppercase">Pricing</div>
          <h2 id="pricing-heading" className="mt-2 text-2xl font-semibold tracking-tight text-balance sm:text-3xl">
            Start free. Grow when your agents do.
          </h2>
          <p className="mt-2 text-sm text-muted-foreground sm:text-base">{EVERY_PLAN}.</p>
        </div>
        {plans.error ? (
          <ErrorState title="Could not load plans" error={plans.error} onRetry={() => plans.refetch()} />
        ) : (
          <PlanGrid
            plans={plans.data}
            loading={plans.isLoading}
            className="mx-auto w-full max-w-5xl"
            renderCta={(p) =>
              p.price_usd_month === 0 ? (
                <Button size="lg" variant="outline" className="h-11" onClick={focusForm}>
                  Start free <ArrowRight />
                </Button>
              ) : (
                <Button
                  size="lg"
                  disabled
                  title="Paid plans are not available yet"
                  className={cn(
                    "h-11",
                    p.id === "team" && "bg-white text-zinc-900 disabled:opacity-80",
                    p.highlight && "bg-gradient-to-r from-[#FF9A4D] to-[#E2452B] text-white disabled:opacity-75",
                  )}
                >
                  Upgrade — coming soon
                </Button>
              )
            }
          />
        )}
        <p className="text-center text-xs text-muted-foreground">
          Every workspace starts on Free. Paid plans are coming soon — you will never be asked for payment details here.
        </p>
      </section>
    </div>
  );
}

function WorkspaceForm({ tokenRequired, onDone }: { tokenRequired: boolean; onDone: (r: SetupResult) => void }) {
  const qc = useQueryClient();
  const [rateLimited, setRateLimited] = useState(false);
  const form = useForm<Values>({
    resolver: zodResolver(
      tokenRequired ? schema.extend({ setup_token: z.string().trim().min(1, "Setup token is required") }) : schema,
    ),
    defaultValues: { name: "", setup_token: "" },
  });
  const { register, handleSubmit, formState, setError } = form;

  const setup = useMutation({
    mutationFn: (v: Values) =>
      api.setup({
        name: v.name.trim() || undefined,
        ...(tokenRequired ? { setup_token: v.setup_token.trim() } : {}),
      }),
    onSuccess: (res) => {
      setCsrfToken(res.csrf_token);
      if (res.api_key.key) rememberApiKey({ name: res.api_key.name, key: res.api_key.key });
      // A new workspace means a new session: drop anything cached for the previous one.
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== "setup" && q.queryKey[0] !== "plans" });
      onDone(res);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.status === 403) {
        setError("setup_token", { message: err.message });
        return;
      }
      if (err instanceof ApiError && err.isRateLimited) {
        setRateLimited(true);
        return;
      }
      if (err instanceof ApiError && err.fields?.name) setError("name", { message: err.fields.name });
      toastApiError(err);
    },
  });

  return (
    <form
      onSubmit={handleSubmit((v) => {
        setRateLimited(false);
        setup.mutate(v);
      })}
      className="relative grid gap-5 rounded-2xl border bg-card/90 p-5 shadow-xl shadow-black/[0.04] backdrop-blur sm:p-7 dark:shadow-black/30"
      noValidate
      aria-label="Create workspace"
    >
      <div>
        <h2 className="text-lg font-semibold tracking-tight">Name your workspace</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          You&apos;ll get an API key for your agents and a recovery key for you.
        </p>
      </div>
      <FieldGroup className="gap-4">
        <Field data-invalid={!!formState.errors.name}>
          <FieldLabel htmlFor={NAME_INPUT_ID}>Workspace name</FieldLabel>
          <Input
            id={NAME_INPUT_ID}
            autoComplete="organization"
            placeholder="e.g. Acme Studio"
            className="h-11 text-base sm:text-sm"
            {...register("name")}
          />
          <FieldError errors={[formState.errors.name]} />
        </Field>
        {tokenRequired && (
          <Field data-invalid={!!formState.errors.setup_token}>
            <FieldLabel htmlFor="setup-token">Setup token</FieldLabel>
            <Input
              id="setup-token"
              type="password"
              autoComplete="off"
              className="h-11 font-mono text-base sm:text-sm"
              aria-invalid={!!formState.errors.setup_token}
              {...register("setup_token")}
            />
            <FieldDescription>This server requires the SETUP_TOKEN configured by its operator.</FieldDescription>
            <FieldError errors={[formState.errors.setup_token]} />
          </Field>
        )}
      </FieldGroup>
      {rateLimited && (
        <div
          role="alert"
          className="rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-amber-900 dark:text-amber-200"
        >
          Too many workspaces were created from your network recently. Please wait a few minutes and try again.
        </div>
      )}
      <Button
        type="submit"
        size="lg"
        disabled={setup.isPending}
        className="h-11 bg-gradient-to-r from-[#FF8A3D] to-[#E2452B] text-white shadow-md shadow-orange-500/25 hover:opacity-95"
      >
        {setup.isPending ? "Creating workspace…" : "Create free workspace"} <ArrowRight />
      </Button>
      <p className="text-center text-xs text-muted-foreground">
        Already have one?{" "}
        <Link href="/login" className="font-medium text-foreground underline-offset-4 hover:underline">
          Sign in with your recovery key
        </Link>
      </p>
    </form>
  );
}

function SecretsStep({ result }: { result: SetupResult }) {
  const router = useRouter();
  const [ack, setAck] = useState(false);
  const apiKey = result.api_key.key ?? "";
  const plan = result.workspace.plan ?? "free";

  function download() {
    downloadText(
      `staqly-${result.workspace.name.toLowerCase().replace(/[^a-z0-9]+/g, "-")}-keys.txt`,
      [
        `staq.ly workspace: ${result.workspace.name} (${result.workspace.id})`,
        `Created: ${result.workspace.created_at}`,
        "",
        `API key (${result.api_key.name}; scopes: ${result.api_key.scopes.join(", ")}):`,
        apiKey,
        "",
        "Recovery key — signs you in to this workspace; the only way back in if all browser sessions are lost:",
        result.recovery_key,
        "",
        "Store this file in a password manager and delete it from your downloads folder.",
      ].join("\n"),
    );
  }

  return (
    <div
      className="grid gap-6 rounded-2xl border bg-card p-5 shadow-xl shadow-black/[0.04] sm:p-7"
      data-testid="setup-secrets"
    >
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Save your keys</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Workspace <span className="font-medium text-foreground">{result.workspace.name}</span> is ready. These keys
          are shown <strong>only once</strong>.
        </p>
        <p className="mt-3 rounded-lg bg-muted/60 px-3 py-2 text-sm leading-relaxed" data-testid="setup-plan">
          <PlanBadge plan={plan} className="mr-1.5 align-[1px]" />
          <span>
            Your workspace is on the <strong>{planName(plan)}</strong> plan
            {plan === "free" ? " — no card needed. Upgrades are coming soon." : "."}
          </span>
        </p>
      </div>

      <div className="grid gap-2">
        <Label className="flex flex-wrap items-center gap-1.5">
          <KeyRound className="size-3.5" /> API key
          <span className="font-normal text-muted-foreground">— for n8n, Claude Code, Codex, scripts</span>
        </Label>
        <SecretField value={apiKey} label="API key" />
      </div>

      <div className="grid gap-2">
        <Label className="flex flex-wrap items-center gap-1.5">
          <LifeBuoy className="size-3.5" /> Recovery key
          <span className="font-normal text-muted-foreground">— for you, keep offline</span>
        </Label>
        <SecretField value={result.recovery_key} label="Recovery key" />
      </div>

      <div
        role="alert"
        className="flex gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-900 dark:text-amber-200"
      >
        <AlertTriangle className="mt-0.5 size-4 shrink-0" />
        <div>
          The recovery key is how you <strong>sign in to this workspace</strong> from a new browser and the only way
          back in if all sessions are lost. staq.ly stores only a hash of it and cannot show it again. Lose both and you
          may be <strong>permanently locked out</strong>.
        </div>
      </div>

      <Button type="button" variant="outline" size="lg" className="h-11" onClick={download}>
        <Download /> Download keys as .txt
      </Button>

      <div className="flex min-h-11 items-start gap-3 rounded-lg border p-3">
        <Checkbox id="ack-recovery" checked={ack} onCheckedChange={(v) => setAck(v === true)} className="mt-0.5" />
        <Label htmlFor="ack-recovery" className="flex-1 leading-snug font-normal">
          I have saved my recovery key
        </Label>
      </div>

      <Button size="lg" className="h-11" disabled={!ack} onClick={() => router.replace("/inbox")}>
        Open dashboard <ArrowRight />
      </Button>
    </div>
  );
}
