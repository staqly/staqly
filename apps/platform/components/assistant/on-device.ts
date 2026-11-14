import type * as WebLLM from '@mlc-ai/web-llm'

import type { AssistantMessage } from '@/lib/api'

// The assistant on this device when the server can't answer (its allowance ran out, its models refused): the
// WebLLM model the server names (AI_ASSISTANT_BROWSER_MODEL) on the GPU, text only, without the workspace's tools.
// The library loads on first use, pinned, from jsDelivr (the CSP in apps/api/internal/web/web.go allows exactly this
// file when the model is set; the npm package is here for its types only); the model downloads once into the
// browser's cache.

export const WEBLLM_CDN = 'https://cdn.jsdelivr.net/npm/@mlc-ai/web-llm@0.2.86/lib/index.js'

function webLLM(): Promise<typeof WebLLM> {
  return import(/* webpackIgnore: true */ WEBLLM_CDN)
}

const SYSTEM =
  "You are staqly's assistant, now running on the person's own device without access to their workspace: you " +
  "can't read, create, schedule or change anything there. Help with writing and editing post texts, ideas and " +
  'questions. If they ask you to do something in the workspace, say it works again once the assistant on the ' +
  "server is back. Be brief and answer in the person's language."

let engine: { model: string; ready: Promise<WebLLM.MLCEngine> } | null = null

/** WebGPU is there (Chrome, Edge, Safari 26+): the only way WebLLM runs. */
export function canRunOnDevice() {
  return typeof navigator !== 'undefined' && 'gpu' in navigator
}

/** The conversation without tool calls and results: the model on the device has no tools. */
export function textOnly(messages: AssistantMessage[]) {
  return messages
    .filter((m) => (m.role === 'user' || m.role === 'assistant') && m.content.trim())
    .map((m) => ({ role: m.role as 'user' | 'assistant', content: m.content }))
}

/** Answers the conversation on this device, streaming the text; onProgress reports the first download (0-1). */
export async function answerOnDevice(
  model: string,
  messages: AssistantMessage[],
  onText: (t: string) => void,
  onProgress: (p: number) => void,
  signal: AbortSignal,
) {
  if (engine?.model !== model) {
    const { CreateMLCEngine } = await webLLM()

    engine = { model, ready: CreateMLCEngine(model, { initProgressCallback: (r) => onProgress(r.progress) }) }
    engine.ready.catch(() => {
      engine = null
    })
  }

  const e = await engine.ready

  signal.addEventListener('abort', () => e.interruptGenerate(), { once: true })

  const stream = await e.chat.completions.create({
    messages: [{ role: 'system', content: SYSTEM }, ...textOnly(messages)],
    stream: true,
  })
  let text = ''

  for await (const chunk of stream) {
    const t = chunk.choices[0]?.delta.content ?? ''

    text += t
    onText(t)
  }

  return text
}
