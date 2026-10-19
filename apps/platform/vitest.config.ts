import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

import { defineConfig } from 'vitest/config'

const require = createRequire(import.meta.url)

/**
 * Compile app code with the React Compiler like `next build` does (`reactCompiler: true`), so tests run the
 * memoised components users get and render counts (tests/render-perf.*.test.tsx) mean something. Uses the
 * Babel that ships with Next; the TypeScript is stripped afterwards by Vite.
 */
function reactCompiler() {
  const babel = require('next/dist/compiled/babel/core') as {
    transformSync: (code: string, opts: object) => { code?: string | null; map?: object | null } | null
  }
  const syntaxTs = require('next/dist/compiled/babel/plugin-syntax-typescript')
  const compiler = require('babel-plugin-react-compiler')

  return {
    name: 'react-compiler',
    enforce: 'pre' as const,
    transform(code: string, id: string) {
      if (
        !/\.tsx?$/.test(id) ||
        id.includes('/node_modules/') ||
        id.includes('/tests/') ||
        !/use[A-Z]|<\w/.test(code)
      ) {
        return null
      }

      const out = babel.transformSync(code, {
        filename: id,
        babelrc: false,
        configFile: false,
        sourceMaps: true,
        plugins: [
          [syntaxTs, { isTSX: id.endsWith('.tsx') }],
          [compiler, {}],
        ],
      })

      return out?.code ? { code: out.code, map: out.map } : null
    },
  }
}

export default defineConfig({
  plugins: [reactCompiler()],
  resolve: {
    alias: {
      '@opengpex/editor': fileURLToPath(new URL('./lib/opengpex', import.meta.url)),
      '@opengpex/components': fileURLToPath(new URL('./lib/opengpex/components', import.meta.url)),
      '@': fileURLToPath(new URL('./', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    // render-count.ts installs a DevTools hook, which must exist before react-dom loads.
    setupFiles: ['./tests/render-count.ts', './tests/setup.ts'],
    include: ['tests/**/*.test.{ts,tsx}'],
    css: false,
    testTimeout: 15000,
  },
})
