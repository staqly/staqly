import { Logo } from '@staqly/brand/logo'
import { Bot, CalendarClock, Send } from 'lucide-react'

const FEATURES = [
  {
    Icon: Send,
    title: 'Write once, post everywhere',
    text: 'One post with its text, pictures and comments goes to every channel you pick.',
  },
  {
    Icon: CalendarClock,
    title: 'A calendar for everything',
    text: 'Schedule, repeat and reschedule posts. Staqly publishes on time and retries when a channel fails.',
  },
  {
    Icon: Bot,
    title: 'Agents welcome',
    text: 'Claude Code, Codex, n8n and scripts write and schedule posts through MCP, the API and the SDKs.',
  },
]

export function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-svh lg:grid-cols-[1.05fr_1fr]">
      <aside className="relative hidden overflow-hidden border-r bg-sidebar lg:flex lg:flex-col lg:justify-between lg:p-12">
        <div className="bg-grid pointer-events-none absolute inset-0 [mask-image:radial-gradient(ellipse_at_top_left,black_30%,transparent_75%)]" />
        <div className="pointer-events-none absolute -top-32 -left-32 size-96 rounded-full bg-brand/20 blur-3xl" />
        <Logo className="relative text-lg" markClassName="size-8" />
        <div className="relative max-w-lg">
          {/* A tagline, not the page's heading: the form beside it has the one h1 ("Sign in..."). */}
          <p className="text-4xl leading-[1.1] font-semibold tracking-tight text-balance">
            Write, schedule and publish posts with your team and your AI agents.
          </p>
          <p className="mt-4 text-base text-muted-foreground">
            The post scheduler for creators and teams. People and agents write the posts, the calendar keeps them in
            order, and Staqly publishes them to Instagram, TikTok, YouTube and the rest.
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
          Open source · Works with Claude Code, Codex and n8n · Your channels, your data
        </div>
      </aside>
      <main className="relative isolate flex min-h-svh flex-col items-center justify-center overflow-x-clip px-4 pt-[max(1.5rem,env(safe-area-inset-top))] pb-[max(1.5rem,env(safe-area-inset-bottom))] sm:p-10">
        <div
          aria-hidden
          className="pointer-events-none absolute top-[-160px] left-1/2 -z-10 h-[360px] w-[140vw] -translate-x-1/2 rounded-full bg-[radial-gradient(closest-side,rgb(60_235_122/0.13),transparent)] blur-2xl lg:hidden"
        />
        <div className="mb-8 lg:hidden">
          <Logo className="text-lg" markClassName="size-8" />
        </div>
        <div className="w-full max-w-md">{children}</div>
      </main>
    </div>
  )
}
