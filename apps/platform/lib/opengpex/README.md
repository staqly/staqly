# OpenGPEX in staqly

The media editor (`/media/edit`) is built on [OpenGPEX](https://github.com/gpex-cloud/opengpex) by The OpenGPEX
Authors, licensed **GPL-3.0-only** (`LICENSE` in this folder; every upstream file keeps its header). staqly as a
whole is AGPL-3.0-only, which GPL-3.0 code may be combined with (GPL-3.0 section 13).

Upstream: `https://github.com/gpex-cloud/opengpex`, version 2.0.22, commit
`05c777a7a056df8e4a66c8211427de51f03e41bf`.

## Where the files came from

| Here | Upstream |
|---|---|
| `core/`, `stage/`, `widgets/`, `workspace/`, `index.css` | `src/lib/opengpex/` (same paths) |
| `plugins/base/` | `src/lib/opengpex/plugins/base/` (only the plugins listed in `core/plugin/registry.ts`) |
| `components/theme/` | `src/components/theme/` |
| `logo.svg` | `public/logo.svg` |
| `core/helpers/gpex-payload.ts` | the payload helpers of `src/lib/opengpex/plugins/base/xtends/CloudMenu/commands.ts` |
| `../../scripts/editor-assets.mjs` | adapted from `scripts/postinstall-exts.mjs` |
| `../../components/media-editor/editor.css` | the animations of `src/app/globals.css` |

## What staqly changed

- **Left out**: the OpenGPEX cloud account and sync (`core/cloud`, CloudMenu), the plugin marketplace and user
  plugins (GpexHubPanel, `plugins/user`, `/api/plugins/*` loading), the update check and its telemetry
  (`core/system`, ReleaseGuideCard), the ComfyUI and LLM bridges (staqly's AI goes through its own server with the
  workspace's key), animated images, and the developer panels (Debug, Layout, Storage, Format strategy). The
  server routes of the upstream app (`src/app/api`) and the RAW and PDF codecs (LibRaw, Ghostscript) are not used.
- `core/plugin/registry.ts`: a kept list instead of upstream's generated one, with staqly's plugin
  (`components/media-editor/bridge-plugin.tsx`).
- `workspace/hooks/usePluginInit.ts`: registers the built-in plugins only.
- `core/motion/tween.ts` (new) replaces GSAP, whose licence is not open source, behind the unchanged
  `core/motion` API (`Motion.to/set/fromTo/timeline/delayedCall/killTweensOf/ticker`).
- staqly's look (no OpenGPEX branding): `workspace/components/ToolMenu.tsx` (a menu icon instead of the
  logo, a Licenses item linking to the docs' legal notice instead of the name, version and GitHub link),
  `plugins/base/overlays/FileLoader/components.tsx` and `workspace/components/LandingPage.tsx` (start
  screen), `workspace/Workspace.tsx` (loading text). Sizes, sentence case and colours are overridden in
  `components/media-editor/editor.css`, plus `sq-quality` on the export quality number
  (`ResizeExportRules.tsx`).
- Settings: the Preferences plugin is left out; Pixel grid, Smart guides and Dock settings no longer add tabs
  (`index.tsx` of those plugins); the dialog is titled "Editor settings" (`SettingsPanel/components.tsx`).
- Accessibility: icon buttons are named from their tooltip (`widgets/Tooltip.tsx`, `FancyButton.tsx`,
  `ActionButton.tsx`, layer Show/Lock buttons), the switch is keyboard-operable with a name
  (`widgets/Switch.tsx`, `MetadataColorControls.tsx`), the export sliders are labelled.
- `core/plugin/version.ts`: the upstream version as a constant (upstream injects it at build time); the tool
  menu and the start screen read it from there.
- `workspace/components/ToolMenu.tsx`: no update check; the logo from `/ext/opengpex-logo.svg`.
- `plugins/base/drawers/AIToolsDrawer/bgremover/protocols.ts` and `upscaler/protocols.ts`: models licensed for
  non-commercial use only are not offered (briaai/RMBG-1.4, Kim2091/2x-AnimeSharpV4).

## On-device AI models

Downloaded from Hugging Face when someone uses the tool, never bundled: InSPyReNet (MIT, background removal),
SAM 2.1 tiny (Apache-2.0, segmentation), LaMa (Apache-2.0, object eraser), Real-ESRGAN (BSD-3-Clause, upscale).
ONNX Runtime Web and transformers.js load from jsDelivr at pinned versions (the server's editor CSP allows only
those paths).

## Updating

There is no sync script: compare upstream's changes since the commit above with these files by hand, carry the
changes listed under "What staqly changed" over, update the commit here, then `pnpm --filter @staqly/platform
typecheck test build` and open `/media/edit`.
