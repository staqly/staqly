import { Check, Sparkles } from "lucide-react";
import { UNLIMITED, type Plan, type PlanLimits } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";
import { SkeletonList } from "@/components/common/skeleton-list";

export const EVERY_PLAN = "Signed webhooks, full history and MCP on every plan";

const TAGLINES: Record<string, string> = {
  free: "Kick the tyres. No card, no email.",
  indie: "For solo builders shipping every day.",
  team: "For teams running agents at scale.",
};

function count(n: number, noun: string) {
  return n === UNLIMITED ? `Unlimited ${noun}` : `${n.toLocaleString("en-US")} ${noun}`;
}

/** Feature lines derived from limits, used when the server does not send any. */
export function limitFeatures(l: PlanLimits): string[] {
  return [
    l.items_per_month === UNLIMITED ? "Unlimited items" : `${l.items_per_month.toLocaleString("en-US")} items / month`,
    count(l.webhooks, "webhooks"),
    count(l.api_keys, "API keys"),
    l.storage_bytes === UNLIMITED ? "Unlimited storage" : `${formatBytes(l.storage_bytes)} storage`,
  ];
}

export function PlanCard({
  plan,
  current,
  cta,
}: {
  plan: Plan;
  current?: boolean;
  /** Call-to-action rendered at the bottom of the price block. */
  cta?: React.ReactNode;
}) {
  const premium = plan.id === "team";
  const highlight = plan.highlight;
  const features = plan.features?.length ? plan.features : limitFeatures(plan.limits);

  const body = (
    <div
      className={cn(
        "relative flex h-full flex-col rounded-[calc(var(--radius)*1.8)] p-5 sm:p-6",
        premium
          ? "bg-[radial-gradient(120%_80%_at_100%_0%,rgb(255_138_61/0.16),transparent_55%),linear-gradient(180deg,#17181c,#0d0e11)] text-zinc-100"
          : "bg-card text-card-foreground",
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <h3 className={cn("text-base font-semibold tracking-tight", premium && "text-white")}>{plan.name}</h3>
        {current ? (
          <span
            className={cn(
              "rounded-full px-2 py-0.5 text-[11px] font-medium",
              premium ? "bg-white/10 text-white" : "bg-foreground text-background",
            )}
          >
            Current plan
          </span>
        ) : highlight ? (
          <span className="inline-flex items-center gap-1 rounded-full bg-gradient-to-r from-[#FF9A4D] to-[#E2452B] px-2 py-0.5 text-[11px] font-semibold text-white shadow-sm shadow-orange-500/30">
            <Sparkles className="size-3" /> Most popular
          </span>
        ) : null}
      </div>
      <p className={cn("mt-1 text-sm", premium ? "text-zinc-400" : "text-muted-foreground")}>
        {TAGLINES[plan.id] ?? ""}
      </p>
      <div className="mt-5 flex items-baseline gap-1">
        <span className={cn("text-4xl font-semibold tracking-tight tabular-nums", premium && "text-white")}>
          ${plan.price_usd_month}
        </span>
        <span className={cn("text-sm", premium ? "text-zinc-400" : "text-muted-foreground")}>/mo</span>
      </div>
      {cta && <div className="mt-5 grid">{cta}</div>}
      <div className={cn("my-5 h-px", premium ? "bg-white/10" : "bg-border")} />
      <ul className="grid gap-2.5 text-sm" aria-label={`${plan.name} features`}>
        {[...features, EVERY_PLAN.replace(" on every plan", "")].map((f) => (
          <li key={f} className="flex items-start gap-2.5">
            <span
              className={cn(
                "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full",
                premium
                  ? "bg-white/10 text-orange-300"
                  : highlight
                    ? "bg-brand text-brand-foreground"
                    : "bg-brand-soft text-brand",
              )}
            >
              <Check className="size-3" strokeWidth={3} />
            </span>
            <span className={premium ? "text-zinc-200" : undefined}>{f}</span>
          </li>
        ))}
      </ul>
    </div>
  );

  return (
    <div
      data-testid={`plan-${plan.id}`}
      data-current={current || undefined}
      className={cn(
        "relative rounded-[calc(var(--radius)*1.8+1px)] p-px",
        highlight
          ? "bg-gradient-to-b from-[#FF9A4D] via-[#F2622B]/70 to-[#D9402A]/30 shadow-xl shadow-orange-500/10"
          : premium
            ? "bg-gradient-to-b from-white/25 to-white/5 shadow-xl shadow-black/20 ring-1 ring-black/40 dark:ring-white/10"
            : "bg-border",
        current && !highlight && !premium && "bg-foreground/60 dark:bg-foreground/50",
      )}
    >
      {body}
    </div>
  );
}

export function PlanGrid({
  plans,
  loading,
  currentPlanId,
  renderCta,
  className,
}: {
  plans?: Plan[];
  loading?: boolean;
  currentPlanId?: string;
  renderCta?: (plan: Plan, current: boolean) => React.ReactNode;
  className?: string;
}) {
  if (loading) {
    return (
      <div className={cn("grid gap-4 md:grid-cols-3", className)}>
        <SkeletonList count={3} className="h-96 rounded-2xl" />
      </div>
    );
  }
  return (
    <div className={cn("grid items-stretch gap-4 md:grid-cols-3", className)} data-testid="plan-grid">
      {(plans ?? []).map((p) => (
        <PlanCard key={p.id} plan={p} current={p.id === currentPlanId} cta={renderCta?.(p, p.id === currentPlanId)} />
      ))}
    </div>
  );
}
