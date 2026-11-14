import { defineConfig } from 'vitest/config'
import { BaseSequencer, type TestSpecification } from 'vitest/node'

// The files tell one story on one stack (local mode, then the same volume in
// cloud mode), so they run one at a time in file name order.
class FileNameSequencer extends BaseSequencer {
  override async sort(files: TestSpecification[]): Promise<TestSpecification[]> {
    return files.toSorted((a, b) => a.moduleId.localeCompare(b.moduleId))
  }
}

export default defineConfig({
  test: {
    include: ['tests/**/*.test.ts'],
    globalSetup: ['./lib/global-setup.ts'],
    fileParallelism: false,
    sequence: { sequencer: FileNameSequencer },
    testTimeout: 120_000,
    hookTimeout: 300_000,
    reporters: ['verbose'],
  },
})
