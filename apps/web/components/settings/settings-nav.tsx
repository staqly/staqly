"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

const LINKS = [
  { href: "/settings", label: "General" },
  { href: "/settings/plan", label: "Plan & usage" },
  { href: "/settings/notifications", label: "Notifications" },
  { href: "/settings/data", label: "Data & privacy" },
  { href: "/settings/system", label: "System" },
];

export function SettingsNav({ className }: { className?: string }) {
  const pathname = usePathname();
  return (
    <nav
      aria-label="Settings sections"
      className={cn("scrollbar-none -mx-4 flex gap-1 overflow-x-auto border-b px-4 sm:mx-0 sm:px-0", className)}
    >
      {LINKS.map((l) => {
        const active = pathname === l.href;
        return (
          <Link
            key={l.href}
            href={l.href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "relative -mb-px inline-flex h-10 shrink-0 items-center px-3 whitespace-nowrap text-sm font-medium transition-colors pointer-coarse:h-11",
              active
                ? "text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-brand"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {l.label}
          </Link>
        );
      })}
    </nav>
  );
}
