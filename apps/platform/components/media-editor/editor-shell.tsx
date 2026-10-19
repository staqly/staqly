'use client'

import { ThemeProvider } from '@opengpex/components/theme/ThemeContext'
import { EditorProvider } from '@opengpex/editor/core/context'
import { Workspace } from '@opengpex/editor/workspace/Workspace'
// The editor itself, loaded only on /media/edit: OpenGPEX's workspace (lib/opengpex) with the staqly bar on top
// and staqly's panels (AI, channels) beside it, one editor state for all of them.
import { LayoutTemplate, Sparkles } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import type { Media } from '@/lib/api'

import { AiPanel } from './ai-panel'
import { ChannelsPanel } from './channels-panel'
import { EditorBar } from './editor-bar'

import './editor.css'

type Panel = 'ai' | 'channels' | null

export default function EditorShell() {
  const [panel, setPanel] = useState<Panel>(null)
  const [sources, setSources] = useState<Media[]>([])
  const toggle = (p: Panel) => setPanel((cur) => (cur === p ? null : p))

  return (
    // OpenGPEX's theme follows the dashboard's (both keep it in localStorage "theme").
    <ThemeProvider initialTheme="system" initialIsDark={false}>
      <EditorProvider>
        <div className="flex h-dvh w-full flex-col">
          <EditorBar
            onSources={setSources}
            panels={
              <>
                <Button
                  variant={panel === 'channels' ? 'secondary' : 'ghost'}
                  aria-pressed={panel === 'channels'}
                  onClick={() => toggle('channels')}
                  data-testid="channels-toggle"
                >
                  <LayoutTemplate aria-hidden /> <span className="max-sm:sr-only">Channels</span>
                </Button>
                <Button
                  variant={panel === 'ai' ? 'secondary' : 'ghost'}
                  aria-pressed={panel === 'ai'}
                  onClick={() => toggle('ai')}
                  data-testid="ai-toggle"
                >
                  <Sparkles aria-hidden /> <span className="max-sm:sr-only">AI</span>
                </Button>
              </>
            }
          />
          <div className="relative isolate min-h-0 flex-1">
            <Workspace />
            {panel === 'ai' && <AiPanel onClose={() => setPanel(null)} />}
            {panel === 'channels' && <ChannelsPanel sources={sources} onClose={() => setPanel(null)} />}
          </div>
        </div>
      </EditorProvider>
    </ThemeProvider>
  )
}
