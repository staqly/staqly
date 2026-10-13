import { cn } from "@/lib/utils";
import { statusMeta } from "@/lib/status";

export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const m = statusMeta(status);
  return (
    <span
      data-status={status}
      title={m.description}
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1.5 rounded-md px-1.5 text-[11px] font-medium ring-1 ring-inset whitespace-nowrap",
        m.badge,
        className,
      )}
    >
      <span className={cn("size-1.5 rounded-full", m.dot, status === "processing" && "animate-pulse")} />
      {m.label}
    </span>
  );
}

export function StatusDot({ status, className }: { status: string; className?: string }) {
  return <span className={cn("inline-block size-2 rounded-full", statusMeta(status).dot, className)} />;
}
