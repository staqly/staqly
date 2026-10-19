// MCP over Streamable HTTP (POST /mcp) and STDIO (`mcp-stdio` in the api container).
import { spawn } from 'node:child_process'

import { http } from './http.js'
import { API, API_CONTAINER } from './stack.js'

export interface JsonRpcResponse<R = Record<string, unknown>> {
  jsonrpc: '2.0'
  id?: number
  result?: R
  error?: { code: number; message: string }
}

export interface ToolResult<S = Record<string, unknown>> {
  isError?: boolean
  structuredContent: S
  content: { type: string; text?: string }[]
}

export const INITIALIZE = {
  jsonrpc: '2.0',
  id: 1,
  method: 'initialize',
  params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } },
}

export const INITIALIZED = { jsonrpc: '2.0', method: 'notifications/initialized' }

export function toolCall(id: number, name: string, args: Record<string, unknown>) {
  return { jsonrpc: '2.0', id, method: 'tools/call', params: { name, arguments: args } }
}

/** The JSON-RPC message of an answer sent as JSON or as a server-sent event. */
function parseAnswer(contentType: string | undefined, text: string): JsonRpcResponse | undefined {
  if (!text) {
    return undefined
  }

  if (contentType?.includes('text/event-stream')) {
    const data = text
      .split('\n')
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trim())
      .pop()

    return data ? (JSON.parse(data) as JsonRpcResponse) : undefined
  }

  return JSON.parse(text) as JsonRpcResponse
}

/** Posts one JSON-RPC message to the Streamable HTTP endpoint. */
export async function mcpHttp(
  message: object,
  headers: Record<string, string> = {},
): Promise<{ status: number; answer: JsonRpcResponse | undefined }> {
  const res = await http(`${API}/mcp`, {
    method: 'POST',
    json: message,
    headers: { Accept: 'application/json, text/event-stream', ...headers },
  })
  const contentType = res.headers['content-type']

  return { status: res.status, answer: res.status < 300 ? parseAnswer(contentType, res.text()) : undefined }
}

/** Calls a tool over HTTP and returns its result (throws on a JSON-RPC error). */
export async function callTool<S = Record<string, unknown>>(
  name: string,
  args: Record<string, unknown>,
  headers: Record<string, string> = {},
): Promise<ToolResult<S>> {
  const { status, answer } = await mcpHttp(toolCall(Date.now(), name, args), headers)

  if (!answer?.result) {
    throw new Error(`tools/call ${name}: HTTP ${status}, ${JSON.stringify(answer)}`)
  }

  return answer.result as unknown as ToolResult<S>
}

/**
 * Pipes JSON-RPC messages into `docker exec -i staqly-e2e mcp-stdio` and
 * collects the answers until the one with `untilId` arrives.
 */
export function mcpStdio(messages: object[], untilId: number, timeout = 30_000): Promise<JsonRpcResponse[]> {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', ['exec', '-i', API_CONTAINER, 'mcp-stdio'], { stdio: ['pipe', 'pipe', 'pipe'] })
    const answers: JsonRpcResponse[] = []
    let buffer = ''
    let stderr = ''
    let done = false

    const finish = (err?: Error) => {
      if (done) {
        return
      }

      done = true
      clearTimeout(timer)
      child.stdin.end()
      child.kill()

      if (err) {
        reject(err)
      } else {
        resolve(answers)
      }
    }

    const timer = setTimeout(
      () => finish(new Error(`mcp-stdio: no answer to id ${untilId}; stderr: ${stderr}`)),
      timeout,
    )

    child.stderr.on('data', (chunk: Buffer) => {
      stderr += chunk.toString()
    })
    child.stdout.on('data', (chunk: Buffer) => {
      buffer += chunk.toString()
      let newline = buffer.indexOf('\n')

      while (newline >= 0) {
        const line = buffer.slice(0, newline).trim()

        buffer = buffer.slice(newline + 1)
        newline = buffer.indexOf('\n')

        if (line) {
          const answer = JSON.parse(line) as JsonRpcResponse

          answers.push(answer)

          if (answer.id === untilId) {
            finish()
          }
        }
      }
    })
    child.on('error', (err) => finish(err))
    child.on('exit', (code) => finish(new Error(`mcp-stdio exited with ${code}; stderr: ${stderr}`)))

    for (const message of messages) {
      child.stdin.write(`${JSON.stringify(message)}\n`)
    }
  })
}
