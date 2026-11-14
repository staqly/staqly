import { ArrowRight } from 'lucide-react'
import type { Metadata } from 'next'
import { notFound } from 'next/navigation'

import { Breadcrumb, CtaButtons, Eyebrow, PageHero, Step } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { UseCaseCards } from '@/components/use-case-cards'
import { breadcrumbLd, ldJson, pageMeta } from '@/lib/seo'
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
    <main id="main" className="flex-1">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: ldJson(
            breadcrumbLd([
              ['staqly', '/'],
              ['Use cases', '/use-cases'],
              [u.title, `/use-cases/${u.slug}`],
            ]),
          ),
        }}
      />
      <PageHero
        eyebrow={
          <Breadcrumb
            items={[
              ['staqly', '/'],
              ['Use cases', '/use-cases'],
            ]}
          />
        }
        title={u.title}
      >
        <p className="mt-6 max-w-2xl text-lg leading-relaxed text-pretty text-muted-foreground">{u.intro}</p>
        <CtaButtons />
      </PageHero>

      <article className={`${wrap} grid gap-14 py-14 sm:py-20 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.05fr)]`}>
        <div className="space-y-12">
          <section>
            <Eyebrow>The problem</Eyebrow>
            <p className="mt-3 text-lg leading-relaxed text-muted-foreground">{u.problem}</p>
          </section>
          <section>
            <Eyebrow>How it runs</Eyebrow>
            <ol className="mt-4 space-y-4">
              {u.steps.map((st, i) => (
                <Step key={st} n={i + 1}>
                  {st}
                </Step>
              ))}
            </ol>
          </section>
          <section className="rounded-2xl border border-[color-mix(in_oklch,var(--brand)_30%,transparent)] bg-brand-soft/30 p-6">
            <Eyebrow>Result</Eyebrow>
            <p className="mt-2 text-lg leading-relaxed text-pretty">{u.result}</p>
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
                    className="group -mx-2 flex items-center justify-between rounded-lg px-2 py-1.5 text-sm pointer-coarse:py-2.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    {label}
                    <ArrowRight className="size-3.5 opacity-0 transition-opacity group-hover:opacity-100 pointer-coarse:opacity-100" />
                  </a>
                </li>
              ))}
            </ul>
          </div>
        </div>
      </article>

      <section className="border-t border-border bg-card/40">
        <div className={`${wrap} py-16 sm:py-20`}>
          <h2 className="text-2xl font-semibold tracking-[-0.03em]">More use cases</h2>
          <div className="mt-8">
            <UseCaseCards items={more} />
          </div>
        </div>
      </section>
    </main>
  )
}
