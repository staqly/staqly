import { vi } from 'vitest'

/** Controllable stand-in for next/navigation. */
export const nav = {
  pathname: '/',
  search: new URLSearchParams(),
  params: {} as Record<string, string>,
  push: vi.fn<(href: string, opts?: unknown) => void>(),
  replace: vi.fn<(href: string, opts?: unknown) => void>(),
  back: vi.fn<() => void>(),
  refresh: vi.fn<() => void>(),
  prefetch: vi.fn<(href: string) => void>(),
  set(pathname: string, search = '') {
    this.pathname = pathname
    this.search = new URLSearchParams(search)
  },
  reset() {
    this.pathname = '/'
    this.search = new URLSearchParams()
    this.params = {}
    this.push.mockReset()
    this.replace.mockReset()
  },
}

// router.replace("/path?query") updates the fake URL so components re-read it on rerender.
function routerReplace(href: string) {
  const [p, q] = href.split('?')

  nav.pathname = p
  nav.search = new URLSearchParams(q ?? '')
}

// One router object, like Next's (useRouter() reads it from context): a fresh object per call would make
// every callback that closes over the router look changed to the React Compiler.
const router = {
  push: nav.push,
  replace: (href: string, opts?: unknown) => {
    routerReplace(href)
    nav.replace(href, opts)
  },
  back: nav.back,
  refresh: nav.refresh,
  prefetch: nav.prefetch,
  forward: vi.fn<() => void>(),
}

export const navigationModule = {
  useRouter: () => router,
  usePathname: () => nav.pathname,
  useSearchParams: () => nav.search,
  useParams: () => nav.params,
  redirect: vi.fn<(href: string) => never>(),
  notFound: vi.fn<() => never>(),
}
