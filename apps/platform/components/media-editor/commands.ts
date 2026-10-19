import { packGpex, unpackGpex } from '@opengpex/editor/core/helpers/gpex-format'
// Editor commands of the staqly plugin: they run with OpenGPEX's full context, so adding a layer or a version
// goes through its history (undo/redo) like any built-in tool.
import { buildPayload, unpackPayload } from '@opengpex/editor/core/helpers/gpex-payload'
import { LayerFactory } from '@opengpex/editor/core/layer'
import type { EditorCommand, EditorContextValue, Frame } from '@opengpex/editor/core/types'

import * as P from './protocols'

export interface OpenMediaPayload {
  files: { blob: Blob; name: string }[]
}

/** The first file opens as the picture (its own size and quality); every other one becomes a layer on it. */
async function openMedia(ctx: EditorContextValue, { files }: OpenMediaPayload) {
  const [first, ...rest] = files

  if (!first) {
    return
  }

  const frameId = await ctx.actions.adv.frame.create.trunk.execute({
    source: new File([first.blob], first.name, { type: first.blob.type }),
  })

  if (!frameId) {
    throw new Error(`Could not open ${first.name}`)
  }

  for (const f of rest) {
    await addLayerTo(ctx, frameId, f)
  }
}

/** A picture as a new layer of the open picture (centred), or as the picture when nothing is open. */
async function addImageLayer(ctx: EditorContextValue, file: { blob: Blob; name: string }) {
  const frameId = ctx.state.activeFrameId

  if (!frameId) {
    await ctx.actions.adv.frame.create.trunk.execute({
      source: new File([file.blob], file.name, { type: file.blob.type }),
    })

    return
  }

  await addLayerTo(ctx, frameId, file)
}

async function addLayerTo(ctx: EditorContextValue, frameId: string, { blob, name }: { blob: Blob; name: string }) {
  // The committed state of a frame created a moment ago: the context this command got is older, and the
  // store commits on the next render.
  let frame: Frame | null = ctx.actions.fast.latestFrame(frameId) ?? ctx.state.frames.byId[frameId] ?? null

  for (let i = 0; !frame && i < 50; i++) {
    await new Promise((r) => setTimeout(r, 50))
    frame = ctx.actions.fast.latestFrame(frameId)
  }

  if (!frame) {
    throw new Error('The picture is not open any more')
  }

  const layer = await ctx.layers.createLayerFromBlob(blob, frame)
  const layers = frame.layers.order.map((id) => frame.layers.byId[id])

  layer.name = LayerFactory.getNewLayerName(layers, name.replace(/\.[a-z0-9]+$/i, '') || 'Picture')
  ctx.layers.addLayer(frame.id, layer)
}

/** A version of the open picture (cropped, padded, expanded...) as a new picture beside it: the original stays. */
async function addVersion(ctx: EditorContextValue, { blob, name }: { blob: Blob; name: string }) {
  return ctx.actions.adv.frame.create.trunk.execute({ source: new File([blob], name, { type: blob.type }) })
}

export interface ExportPayload {
  format?: 'image/png' | 'image/jpeg' | 'image/webp'
  quality?: number
  frameId?: string
}

/** The open picture flattened to a file, at its full size. */
async function exportImage(ctx: EditorContextValue, p: ExportPayload = {}): Promise<Blob> {
  const frame = p.frameId ? ctx.state.frames.byId[p.frameId] : ctx.activeFrame

  if (!frame) {
    throw new Error('Nothing to save: open a picture first')
  }

  if (frame.id !== ctx.activeFrame?.id) {
    ctx.actions.switchFrame(frame.id)
  }

  const res = await ctx.actions.adv.frame.export.encode.execute({
    exportWidth: frame.canvas.w,
    exportHeight: frame.canvas.h,
    config: { format: p.format ?? 'image/png', quality: p.quality ?? 92 },
  })

  return res.blob
}

/** The open picture as an editable project (OpenGPEX's .gpex container: layers, text, masks, history-free). */
async function pack(ctx: EditorContextValue): Promise<Blob> {
  const frame = ctx.activeFrame

  if (!frame) {
    throw new Error('Nothing to save: open a picture first')
  }

  const exported = await ctx.actions.adv.gpex.pack.execute(frame)
  const payload = await buildPayload(exported)
  const buffer = packGpex(
    new Uint8Array(0),
    {
      format: 'gpex',
      version: 1,
      gpexVersion: 'v2',
      frameLocalId: frame.id,
      frameName: frame.name || 'Untitled',
      canvasWidth: frame.canvas.w,
      canvasHeight: frame.canvas.h,
      layerCount: LayerFactory.getHostLayers(frame.layers.order.map((id) => frame.layers.byId[id])).length,
      assetCount: Object.keys(exported.assets).length,
      dpi: frame.dpi ?? 72,
      editorVersion: ctx.coreVersion,
    },
    payload,
  )

  return new Blob([buffer], { type: 'application/x-gpex' })
}

/** Reopens a saved project with its layers. */
async function unpack(ctx: EditorContextValue, file: Blob) {
  const { payload } = unpackGpex(await file.arrayBuffer())
  const unpacked = unpackPayload(payload)

  return ctx.actions.adv.gpex.unpack.execute({
    state: unpacked.state,
    assetBlobs: unpacked.assetBlobs,
    rawBlobs: unpacked.rawBlobs,
    decBuffers: unpacked.decBuffers,
    manifest: unpacked.manifest,
    switchFrame: true,
  })
}

export const STAQLY_COMMANDS = {
  openMedia: { id: P.CMD_OPEN_MEDIA, name: 'Open from the media library', execute: openMedia } as EditorCommand<
    OpenMediaPayload,
    Promise<void>
  >,
  addImageLayer: {
    id: P.CMD_ADD_IMAGE_LAYER,
    name: 'Add a picture as a layer',
    undoable: true,
    execute: addImageLayer,
  } as EditorCommand<{ blob: Blob; name: string }, Promise<void>>,
  addVersion: { id: P.CMD_ADD_VERSION, name: 'Add a version', execute: addVersion } as EditorCommand<
    { blob: Blob; name: string },
    Promise<string>
  >,
  exportImage: { id: P.CMD_EXPORT, name: 'Export the picture', execute: exportImage } as EditorCommand<
    ExportPayload,
    Promise<Blob>
  >,
  pack: { id: P.CMD_PACK, name: 'Pack the project', execute: pack } as EditorCommand<void, Promise<Blob>>,
  unpack: { id: P.CMD_UNPACK, name: 'Open a project', execute: unpack } as EditorCommand<Blob, Promise<unknown>>,
}
