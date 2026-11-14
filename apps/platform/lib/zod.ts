// Forms take `zodResolver` from here, not from @hookform/resolvers, so zod is configured before any schema
// parses. Zod probes for `new Function` to JIT-compile object schemas; our CSP has no 'unsafe-eval', so the
// probe would be reported as a CSP violation. Setting it here keeps zod in the chunks of the forms (set in
// the providers, it put ~15 KB of zod/v4/core into the JavaScript of every route). Don't re-export `z`
// from here: a re-exported namespace is bundled whole, every locale included (~100 KB gzipped).
import { config } from 'zod/v4/core'

config({ jitless: true })

export { zodResolver } from '@hookform/resolvers/zod'
