import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import type { Seed } from './seed.js'

export const STATE_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../.state')

export interface ServerState {
  api: string
  dash: string
  dataDir: string
  pid: number
  seed: Seed
}

export interface State {
  bin: string
  receiver: string
  local: ServerState
  cloud: ServerState & {
    adminKey: string
    recoveryKey: string
    agentKey: string
    readOnlyKey: string
    setupToken: string
  }
}

let cached: State | undefined

export function state(): State {
  cached ??= JSON.parse(readFileSync(join(STATE_DIR, 'state.json'), 'utf8')) as State

  return cached
}
