"use client";

import type { ReactNode } from "react";
import { LogIn, QrCode, Smartphone, Terminal } from "lucide-react";
import { PublicShell } from "@/components/auth/public-shell";
import { LogoMark } from "@/components/common/logo";
import { ErrorState } from "@/components/common/error-state";
import { ApiError } from "@/lib/api";
import { useSession, useSetupStatus } from "@/lib/queries";

const code = "rounded bg-muted px-1 py-0.5 font-mono text-[0.85em] text-foreground";

/**
 * Local mode: the dashboard is published on the network, so every browser —
 * including one on the computer running staq.ly — signs in once with a
 * one-time link (make connect / make up) or a QR code (403 not_local).
 */
function NotConnected() {
  return (
    <PublicShell className="flex max-w-md flex-col justify-center pt-6">
      <div
        className="grid gap-5 rounded-2xl border bg-card p-6 text-center shadow-xl shadow-black/[0.04]"
        data-testid="not-connected"
      >
        <span className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-brand-soft text-brand">
          <LogIn className="size-7" />
        </span>
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Sign in this browser</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            On this computer run <code className={code}>make connect</code> (or <code className={code}>make up</code>)
            and open the link — or scan a QR from a browser that&apos;s already signed in.
          </p>
        </div>
        <ol className="grid gap-3 rounded-2xl bg-muted/50 p-4 text-left text-sm">
          {[
            {
              id: "this-computer",
              Icon: Terminal,
              body: (
                <>
                  <strong className="font-medium">On this computer:</strong> run{" "}
                  <code className={code}>make connect</code> in the staq.ly folder and open the sign-in link it prints.{" "}
                  <code className={code}>make up</code> opens one automatically.
                </>
              ),
            },
            {
              id: "other-device",
              Icon: QrCode,
              body: (
                <>
                  <strong className="font-medium">On a phone or another device:</strong> in a browser that&apos;s
                  already signed in, open <strong className="font-medium">Connect a device</strong> (workspace menu or
                  Settings → Devices) and scan the QR code.
                </>
              ),
            },
          ].map(({ id, Icon, body }) => (
            <li key={id} className="flex items-start gap-3">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-background text-muted-foreground shadow-xs">
                <Icon className="size-4" />
              </span>
              <span className="pt-0.5">{body}</span>
            </li>
          ))}
        </ol>
        <div className="flex items-center justify-center gap-2 text-xs text-muted-foreground">
          <Smartphone className="size-4" /> Each link works once and expires after 5 minutes.
        </div>
      </div>
    </PublicShell>
  );
}

function Splash() {
  return (
    <div className="flex min-h-svh items-center justify-center">
      <div className="flex flex-col items-center gap-3 text-sm text-muted-foreground">
        <LogoMark className="size-9 animate-pulse" />
        Loading workspace…
      </div>
    </div>
  );
}

/**
 * Blocks the dashboard until the session is confirmed. A 401 is turned into a
 * redirect to /login or /setup by the global handler in <Providers>.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const session = useSession();
  const setup = useSetupStatus();
  if (session.isPending) return <Splash />;
  if (session.error) {
    if (session.error instanceof ApiError && session.error.code === "not_local") return <NotConnected />;
    const unauthorized = session.error instanceof ApiError && session.error.status === 401;
    // In cloud mode a 401 becomes a redirect to /login or /setup (see <Providers>).
    if (unauthorized && setup.data?.mode !== "local") return <Splash />;
    return (
      <div className="flex min-h-svh items-center justify-center p-6">
        <ErrorState
          className="max-w-md"
          title={
            setup.data?.mode === "local"
              ? "This browser is not allowed to use the local workspace"
              : "Cannot reach staq.ly"
          }
          error={session.error}
          onRetry={() => session.refetch()}
        />
      </div>
    );
  }
  return <>{children}</>;
}
