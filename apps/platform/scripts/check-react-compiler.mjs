// A successful build can silently skip React Compiler optimisation. Keep every component and hook compilable.
import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const require = createRequire(import.meta.url)
const babel = require('next/dist/compiled/babel/core')
const syntaxTs = require('next/dist/compiled/babel/plugin-syntax-typescript')
const compiler = require('babel-plugin-react-compiler')
const root = fileURLToPath(new URL('../', import.meta.url))
let compiled = 0
let failures = 0

for (const dir of ['app', 'components', 'hooks', 'lib']) {
  for (const entry of readdirSync(join(root, dir), { recursive: true }).toSorted()) {
    const file = join(dir, entry)

    if (!/\.tsx?$/.test(file) || file.endsWith('api-types.ts')) {
      continue
    }

    babel.transformSync(readFileSync(join(root, file), 'utf8'), {
      filename: file,
      babelrc: false,
      configFile: false,
      plugins: [
        [syntaxTs, { isTSX: file.endsWith('.tsx') }],
        [
          compiler,
          {
            logger: {
              logEvent(filename, event) {
                if (event.kind === 'CompileSuccess') {
                  compiled++
                }

                if (event.kind === 'CompileError') {
                  failures++
                  const { reason, loc } = event.detail.options

                  console.error(`${filename}:${loc?.start?.line ?? '?'}: ${reason}`)
                }
              },
            },
          },
        ],
      ],
    })
  }
}

console.log(`React Compiler: ${compiled} compiled, ${failures} failures.`)
process.exitCode = failures > 0 ? 1 : 0
