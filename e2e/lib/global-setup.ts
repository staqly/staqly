// Brings the e2e stack up before the first test file and removes it (with its
// volume) after the last one.
//   E2E_BUILD=0  use the prebuilt staqly-e2e-{api,dashboard}:test images (CI)
//   E2E_KEEP=1   leave the stack running afterwards
import { compose, composeVisible, docker, waitForStack } from './stack.js'

async function removeLeftovers(): Promise<void> {
  // The receiver of the former bash suite ran outside compose.
  await docker(['rm', '-f', 'staqly-e2e-receiver']).catch(() => '')
  await compose(['down', '-v', '--remove-orphans'])
}

export default async function setup(): Promise<() => Promise<void>> {
  const build = process.env.E2E_BUILD !== '0'

  await removeLeftovers()
  await composeVisible(['up', '-d', '--wait', '--wait-timeout', '300', ...(build ? ['--build'] : ['--no-build'])])
  await waitForStack()

  return async () => {
    if (process.env.E2E_KEEP === '1') {
      console.log('E2E_KEEP=1: the staqly-e2e stack is still running (API :13333, dashboard :18080)')

      return
    }

    await compose(['down', '-v', '--remove-orphans'])
  }
}
