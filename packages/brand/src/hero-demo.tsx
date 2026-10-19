import { HeroDemoClient } from './hero-demo-client'
import { scenarios } from './hero-scenarios'
import { ProviderTile } from './providers'

/**
 * The hero demo (src/hero-demo-client.tsx). The provider tiles it shows render here, on the server, and reach the
 * client as markup: the loop's JavaScript then carries no brand marks (every provider's icon is ~12 KB gzipped).
 * A server component, so it is used from server components (both sites' home pages).
 */
export function HeroDemo({ basePath = '' }: { basePath?: string }) {
  const ids = new Map(scenarios.flatMap((s) => s.targets.map((t) => [t.id, t.channel] as const)))
  const tiles = Object.fromEntries(
    [...ids].map(([id, name]) => [id, <ProviderTile key={id} id={id} name={name} className="size-5 rounded-md" />]),
  )

  return <HeroDemoClient basePath={basePath} tiles={tiles} />
}
