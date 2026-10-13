import { cn } from "@/lib/cn";

/**
 * staq.ly glyph: three stacked layers on an ember tile — the same artwork as the
 * dashboard (apps/web/components/common/logo.tsx). Rendered from /icon.svg so the
 * gradients never clash when several copies are on the page (inline SVG gradient
 * ids break when one copy sits in a hidden container).
 */
export function LogoMark({ className }: { className?: string }) {
  return (
    // oxlint-disable-next-line nextjs/no-img-element -- tiny static SVG; next/image adds nothing in a static export
    <img
      src="/icon.svg"
      alt=""
      aria-hidden
      width={28}
      height={28}
      className={cn("inline-block size-7 shrink-0 drop-shadow-[0_1px_2px_rgb(234_88_12/0.35)]", className)}
    />
  );
}

/** "staq.ly" with the dot in the brand colour. */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("font-semibold tracking-[-0.03em]", className)} aria-label="staq.ly">
      <span aria-hidden>
        staq<span className="text-brand">.</span>ly
      </span>
    </span>
  );
}

export function Logo({ className, docs = true }: { className?: string; docs?: boolean }) {
  return (
    <span className={cn("inline-flex items-center gap-2", className)}>
      <LogoMark className="size-6" />
      <Wordmark className="text-[15px]" />
      {docs && (
        <span className="rounded-md border border-fd-border bg-fd-muted px-1.5 py-px text-[11px] font-medium text-fd-muted-foreground">
          docs
        </span>
      )}
    </span>
  );
}
