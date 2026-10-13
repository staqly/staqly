import { useId } from "react";
import { cn } from "@/lib/utils";

/**
 * staq.ly glyph: three stacked layers on an ember tile. The top layer is solid,
 * the ones underneath fade out — "everything your agents make, stacked up".
 * Kept in sync with app/icon.svg.
 */
export function LogoMark({ className }: { className?: string }) {
  const id = useId().replace(/:/g, "");
  return (
    <svg
      viewBox="0 0 32 32"
      className={cn("inline-block size-7 shrink-0 drop-shadow-[0_1px_2px_rgb(234_88_12/0.35)]", className)}
      aria-hidden
      focusable="false"
    >
      <defs>
        <linearGradient id={`${id}-bg`} x1="4" y1="2" x2="28" y2="30" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#FF9A4D" />
          <stop offset="0.55" stopColor="#F2622B" />
          <stop offset="1" stopColor="#D9402A" />
        </linearGradient>
        <linearGradient id={`${id}-hl`} x1="16" y1="0" x2="16" y2="16" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#fff" stopOpacity="0.28" />
          <stop offset="1" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8.5" fill={`url(#${id}-bg)`} />
      <rect width="32" height="16" rx="8.5" fill={`url(#${id}-hl)`} />
      <path
        d="M7.5 19.6 16 23.9l8.5-4.3"
        fill="none"
        stroke="#fff"
        strokeOpacity="0.45"
        strokeWidth="2.1"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M7.5 15.6 16 19.9l8.5-4.3"
        fill="none"
        stroke="#fff"
        strokeOpacity="0.72"
        strokeWidth="2.1"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M16 6.9 24.5 11.2 16 15.5 7.5 11.2Z"
        fill="#fff"
        stroke="#fff"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  );
}

/** "staq.ly" set in the UI face, with the dot in the brand colour. */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("font-semibold tracking-[-0.03em]", className)} aria-label="staq.ly">
      <span aria-hidden>
        staq<span className="text-brand">.</span>ly
      </span>
    </span>
  );
}

export function Logo({ className, markClassName }: { className?: string; markClassName?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2", className)}>
      <LogoMark className={markClassName} />
      <Wordmark />
    </span>
  );
}
