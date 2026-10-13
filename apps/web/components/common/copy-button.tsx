"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fallback for non-secure contexts (http on a LAN IP).
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  }
}

export function CopyButton({
  value,
  label = "Copy",
  className,
  variant = "ghost",
  size = "icon-sm",
  showLabel = false,
  onCopied,
}: {
  value: string;
  label?: string;
  className?: string;
  variant?: "ghost" | "outline" | "secondary" | "default";
  size?: "icon-sm" | "icon-xs" | "sm" | "xs" | "default";
  showLabel?: boolean;
  onCopied?: () => void;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      type="button"
      variant={variant}
      size={size}
      className={cn("shrink-0", className)}
      aria-label={label}
      onClick={async () => {
        const ok = await copyText(value);
        if (ok) {
          setCopied(true);
          onCopied?.();
          setTimeout(() => setCopied(false), 1500);
        } else {
          toast.error("Could not copy to clipboard");
        }
      }}
    >
      {copied ? <Check className="text-emerald-500" /> : <Copy />}
      {showLabel && <span>{copied ? "Copied" : label}</span>}
    </Button>
  );
}
