import { Bot, CheckCheck, Webhook } from "lucide-react";
import { Logo } from "@/components/common/logo";

const FEATURES = [
  {
    Icon: Bot,
    title: "One place for agent output",
    text: "n8n, Claude Code, Codex and scripts drop images, video, text, links and JSON into collections.",
  },
  {
    Icon: CheckCheck,
    title: "Keyboard-first review",
    text: "Approve, reject with feedback or edit in seconds — j/k, a, r, e.",
  },
  {
    Icon: Webhook,
    title: "Close the loop",
    text: "Signed webhooks and an MCP server tell your agents what you decided.",
  },
];

export function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-svh lg:grid-cols-[1.05fr_1fr]">
      <aside className="relative hidden overflow-hidden border-r bg-sidebar lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div className="bg-grid pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_at_top_left,black_30%,transparent_75%)]" />
        <div className="pointer-events-none absolute -top-32 -left-32 size-96 rounded-full bg-brand/20 blur-3xl" />
        <Logo className="relative text-lg" markClassName="size-8" />
        <div className="relative max-w-lg">
          <h1 className="text-4xl leading-[1.1] font-semibold tracking-tight text-balance">
            The workspace for everything your AI agents create.
          </h1>
          <p className="mt-4 text-base text-muted-foreground">
            staq.ly is a local-first inbox for generated content. Agents store their work; you review, approve and send
            feedback — and they pick it up automatically.
          </p>
          <ul className="mt-10 grid gap-5">
            {FEATURES.map(({ Icon, title, text }) => (
              <li key={title} className="flex gap-3">
                <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border bg-background shadow-xs">
                  <Icon className="size-4 text-brand" />
                </span>
                <div>
                  <div className="text-sm font-medium">{title}</div>
                  <div className="text-sm text-muted-foreground">{text}</div>
                </div>
              </li>
            ))}
          </ul>
        </div>
        <div className="relative text-xs text-muted-foreground">
          Local-first · Self-host or cloud · Your data stays yours
        </div>
      </aside>
      <main className="relative isolate flex min-h-svh flex-col items-center justify-center overflow-x-clip px-4 pt-[max(1.5rem,env(safe-area-inset-top))] pb-[max(1.5rem,env(safe-area-inset-bottom))] sm:p-10">
        <div
          aria-hidden
          className="pointer-events-none absolute top-[-160px] left-1/2 -z-10 h-[360px] w-[140vw] -translate-x-1/2 rounded-full bg-[radial-gradient(closest-side,rgb(255_138_61/0.18),transparent)] blur-2xl lg:hidden"
        />
        <div className="mb-8 lg:hidden">
          <Logo className="text-lg" markClassName="size-8" />
        </div>
        <div className="w-full max-w-md">{children}</div>
      </main>
    </div>
  );
}
