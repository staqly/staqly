'use client'

import dynamic from 'next/dynamic'

/** The post's drawer, loaded on first open; one import for every page that opens posts in place (one chunk). */
export const PostSheet = dynamic(() => import('./post-sheet').then((m) => m.PostSheet), { ssr: false })
