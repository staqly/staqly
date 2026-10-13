'use client'

import { useStaticSearch } from 'fumadocs-core/search/client'
import {
  SearchDialog,
  SearchDialogClose,
  SearchDialogContent,
  SearchDialogHeader,
  SearchDialogIcon,
  SearchDialogInput,
  SearchDialogList,
  SearchDialogOverlay,
  type SharedProps,
} from 'fumadocs-ui/components/dialog/search'

/** Static (in-browser) Orama search over the index exported at /api/search. */
export default function DefaultSearchDialog(props: SharedProps) {
  // Explicit: fumadocs derives the default from Vite's BASE_URL, not Next's basePath.
  const search = useStaticSearch({ from: `${process.env.NEXT_PUBLIC_BASE_PATH}/api/search` })

  return (
    <SearchDialog {...search} {...props}>
      <SearchDialogOverlay />
      <SearchDialogContent>
        <SearchDialogHeader>
          <SearchDialogIcon />
          <SearchDialogInput />
          <SearchDialogClose />
        </SearchDialogHeader>
        <SearchDialogList />
      </SearchDialogContent>
    </SearchDialog>
  )
}
