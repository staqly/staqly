/**
 * OpenGPEX - An Open-source, Web-based Graphics and Photo editor.
 * Copyright (C) 2026 The OpenGPEX Authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, version 3 of the License.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 *
 * SPDX-License-Identifier: GPL-3.0-only
 */

// staqly: the .gpex payload helpers of upstream's CloudMenu plugin (plugins/base/xtends/CloudMenu/commands.ts),
// kept on their own: staqly stores projects in its media library instead of OpenGPEX's cloud.

import { zipSync, unzipSync } from 'fflate';

import type { FrameExportResult } from '@opengpex/editor/core/types';
import type { GpexAssetManifest } from '@opengpex/editor/core/helpers/gpex-format';

export async function buildPayload(exported: FrameExportResult): Promise<Uint8Array> {
  const stateBytes = new TextEncoder().encode(JSON.stringify(exported.state));

  const files: Record<string, Uint8Array> = {
    'state.json': stateBytes,
    'assets-manifest.json': new TextEncoder().encode(JSON.stringify(exported.manifest)),
  };

  for (const [id, blob] of Object.entries(exported.assets)) {
    const ext = blob.type.includes('png') ? 'png' : 'webp';
    files[`assets/${id}.${ext}`] = new Uint8Array(await blob.arrayBuffer());
  }

  // Category ②: original source files, already deduplicated by content hash
  // upstream — one physical copy per file, shared by every page it decoded to.
  for (const [fileHash, blob] of Object.entries(exported.rawBlobs)) {
    files[`raw/${fileHash}`] = new Uint8Array(await blob.arrayBuffer());
  }

  // Category ③: bake products' naked high-depth pixels, written as the bare
  // TypedArray bytes. The element type is NOT encoded here — the reader takes
  // it from the manifest's `colorIdentity.dataFormat`, the single authority.
  for (const [id, buffer] of Object.entries(exported.decBuffers)) {
    files[`dec/${id}.bin`] = new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength);
  }

  return zipSync(files, { level: 0 });
}

export interface UnpackedPayload {
  state: unknown;
  assetBlobs: Record<string, Blob>;
  rawBlobs: Record<string, Blob>;
  decBuffers: Record<string, Uint16Array | Float32Array>;
  /** Undefined for legacy containers written before `assets-manifest.json`. */
  manifest?: GpexAssetManifest;
}

export function unpackPayload(payload: ArrayBuffer): UnpackedPayload {
  const zipData = unzipSync(new Uint8Array(payload));

  const stateJsonBytes = zipData['state.json'];
  if (!stateJsonBytes) throw new Error('Invalid .gpex payload: missing state.json');
  const state = JSON.parse(new TextDecoder().decode(stateJsonBytes));

  // Read the asset manifest first — `dec/` entries cannot be interpreted
  // without each asset's `dataFormat`. Absent ⇒ legacy container, and
  // `unpack` degrades every asset to category ①.
  let manifest: GpexAssetManifest | undefined;
  const manifestBytes = zipData['assets-manifest.json'];
  if (manifestBytes && manifestBytes.byteLength > 0) {
    try {
      manifest = JSON.parse(new TextDecoder().decode(manifestBytes)) as GpexAssetManifest;
    } catch (err) {
      console.warn('[CloudMenu] Malformed assets-manifest.json, falling back to legacy unpack:', err);
    }
  }

  const assetBlobs: Record<string, Blob> = {};
  const rawBlobs: Record<string, Blob> = {};
  const decBuffers: Record<string, Uint16Array | Float32Array> = {};

  for (const [path, data] of Object.entries(zipData)) {
    if (data.byteLength === 0) continue;

    if (path.startsWith('assets/')) {
      const id = path.replace('assets/', '').replace(/\.[^.]+$/, '');
      const ext = path.split('.').pop() || 'png';
      const mimeType = manifest?.assets[id]?.mimeType
        ?? (ext === 'png' ? 'image/png' : 'image/webp');
      assetBlobs[id] = new Blob([data], { type: mimeType });
    } else if (path.startsWith('raw/')) {
      // No extension and no reliable MIME: format routing on the far side goes
      // through the manifest's `sourceFileName`, not this blob's type.
      rawBlobs[path.slice('raw/'.length)] = new Blob([data], { type: 'application/octet-stream' });
    } else if (path.startsWith('dec/') && path.endsWith('.bin')) {
      const id = path.slice('dec/'.length, -'.bin'.length);
      // Copy into a fresh 0-offset buffer: the unzip output is a view into a
      // shared arena whose byteOffset need not satisfy TypedArray alignment.
      const bytes = data.slice();
      decBuffers[id] = manifest?.assets[id]?.colorIdentity.dataFormat === 'rgba32float'
        ? new Float32Array(bytes.buffer)
        : new Uint16Array(bytes.buffer);
    }
  }

  return { state, assetBlobs, rawBlobs, decBuffers, manifest };
}

