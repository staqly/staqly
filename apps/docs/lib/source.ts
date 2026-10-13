import { llms, loader } from 'fumadocs-core/source'

import { docs } from './collections'
import { openapi } from './openapi'
import { docsRoute } from './shared'

// Docs pages + one virtual page per OpenAPI tag under /docs/api/reference.
export const source = loader(
  {
    docs: docs.toFumadocsSource(),
    openapi: await openapi.staticSource({
      baseDir: 'api/reference',
      per: 'tag',
    }),
  },
  {
    baseUrl: docsRoute,
    plugins: [openapi.loaderPlugin()],
  },
)

export const docsLlms = llms(source, {
  renderPage: async (page) => {
    if (page.type === 'openapi') {
      return `# ${page.data.title} (${page.url})\n\nSee the OpenAPI contract: /openapi.yaml`
    }

    return `# ${page.data.title} (${page.url})\n\n${await page.data.getText('processed')}`
  },
})
