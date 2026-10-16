import { ArrowRight } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound } from 'next/navigation'

import { CtaButtons, Eyebrow, PageHero } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { UseCaseCards } from '@/components/use-case-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { allCases } from '@/lib/use-cases'

type Props = { params: Promise<{ slug: string }> }

export const dynamicParams = false

export function generateStaticParams() {
  return allCases.map((u) => ({ slug: u.slug }))
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { slug } = await params
  const u = allCases.find((x) => x.slug === slug)

  return u ? pageMeta(u.title, u.summary, `/use-cases/${u.slug}`) : {}
}

export default async function UseCasePage({ params }: Props) {
  const { slug } = await params
  const u = allCases.find((x) => x.slug === slug)

  if (!u) {
    notFound()
  }

  const more = allCases.filter((x) => x.slug !== slug).slice(0, 3)

  return (
    <main className="flex-1">
      <PageHero
        eyebrow={
          <>
            <Link href="/" className="hover:text-foreground">
              staqly
            </Link>{' '}
            ›{' '}
            <Link href="/use-cases" className="hover:text-foreground">
              Use cases
            </Link>
          </>
        }
        title={u.title}
      >
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">{u.intro}</p>
        <CtaButtons />
      </PageHero>

      <article className={`${wrap} grid gap-14 py-12 sm:py-16 lg:grid-cols-[1fr_1.05fr]`}>
        <div className="space-y-12">
          <section>
            <Eyebrow>The problem</Eyebrow>
            <p className="mt-3 leading-relaxed text-muted-foreground">{u.problem}</p>
          </section>
          <section>
            <Eyebrow>How it runs</Eyebrow>
            <ol className="mt-4 space-y-3">
              {u.steps.map((s, i) => (
                <li key={s} className="flex gap-4">
                  <span className="flex size-7 shrink-0 items-center justify-center rounded-lg border border-border bg-card font-mono text-xs text-brand">
                    {i + 1}
                  </span>
                  <span className="pt-0.5 text-sm leading-relaxed">{s}</span>
                </li>
              ))}
            </ol>
          </section>
          <section>
            <Eyebrow>Result</Eyebrow>
            <p className="mt-3 leading-relaxed text-muted-foreground">{u.result}</p>
          </section>
        </div>
        <div className="min-w-0 space-y-6 lg:sticky lg:top-24 lg:self-start">
          <TerminalCard title={u.code.title} code={u.code.body} />
          <div className="rounded-2xl border border-border bg-card p-5">
            <p className="text-sm font-semibold">Read the full recipe</p>
            <ul className="mt-2 space-y-0.5">
              {u.docs.map(([label, href]) => (
                <li key={href}>
                  <a
                    href={DOCS + href}
                    className="group -mx-2 flex items-center justify-between rounded-lg px-2 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    {label}
                    <ArrowRight className="size-3.5 opacity-0 transition-opacity group-hover:opacity-100" />
                  </a>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </article>

      <section className="border-t border-border bg-card/50">
        <div className={`${wrap} py-12 sm:py-16`}>
          <h2 className="text-2xl font-semibold tracking-[-0.03em]">More use cases</h2>
          <div className="mt-8">
            <UseCaseCards items={more} />
          </div>
        </div>
      </section>
    </main>
  )
}
