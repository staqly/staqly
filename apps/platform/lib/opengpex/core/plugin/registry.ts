/**
 * The editor's plugins (staqly): OpenGPEX's generated registry turned into a hand-kept list of the base plugins
 * a social media editor uses, plus staqly's bridge (components/media-editor). Upstream's account, marketplace,
 * ComfyUI/LLM bridges, animated-image and developer panels are not part of this copy.
 */

import { registerVectorSourceContributor } from '../../core/engine/pipeline/scene/vectorSources';

import { plugin as plugin_base_backstage_FontLoader } from '../../plugins/base/backstage/FontLoader';
import { plugin as plugin_base_backstage_TimeTraveler } from '../../plugins/base/backstage/TimeTraveler';
import { plugin as plugin_base_drawers_AIToolsDrawer } from '../../plugins/base/drawers/AIToolsDrawer';
import { plugin as plugin_base_drawers_AdjustmentDrawer } from '../../plugins/base/drawers/AdjustmentDrawer';
import { plugin as plugin_base_drawers_CraftDrawer } from '../../plugins/base/drawers/CraftDrawer';
import { plugin as plugin_base_drawers_ImageInfoDrawer } from '../../plugins/base/drawers/ImageInfoDrawer';
import { plugin as plugin_base_drawers_LayersDrawer } from '../../plugins/base/drawers/LayersDrawer';
import { plugin as plugin_base_options_ClipOptions } from '../../plugins/base/options/ClipOptions';
import { plugin as plugin_base_options_ColorOptions } from '../../plugins/base/options/ColorOptions';
import { plugin as plugin_base_options_ViewportOptions } from '../../plugins/base/options/ViewportOptions';
import { plugin as plugin_base_overlays_BrushOverlay } from '../../plugins/base/overlays/BrushOverlay';
import { plugin as plugin_base_overlays_ClipOverlay } from '../../plugins/base/overlays/ClipOverlay';
import { plugin as plugin_base_overlays_EraserOverlay } from '../../plugins/base/overlays/EraserOverlay';
import { plugin as plugin_base_overlays_FileLoader } from '../../plugins/base/overlays/FileLoader';
import { plugin as plugin_base_overlays_LayerOverlay } from '../../plugins/base/overlays/LayerOverlay';
import { plugin as plugin_base_overlays_MarkerOverlay } from '../../plugins/base/overlays/MarkerOverlay';
import { plugin as plugin_base_overlays_MosaicOverlay } from '../../plugins/base/overlays/MosaicOverlay';
import { plugin as plugin_base_overlays_PixelGridOverlay } from '../../plugins/base/overlays/PixelGridOverlay';
import { plugin as plugin_base_overlays_SmartGuides } from '../../plugins/base/overlays/SmartGuides';
import { plugin as plugin_base_overlays_TabDock } from '../../plugins/base/overlays/TabDock';
import { plugin as plugin_base_overlays_TextOverlay } from '../../plugins/base/overlays/TextOverlay';
import { plugin as plugin_base_panels_SettingsPanel } from '../../plugins/base/panels/SettingsPanel';
import { plugin as plugin_base_panels_UserGuidePanel } from '../../plugins/base/panels/UserGuidePanel';
import { plugin as plugin_staqly_bridge } from '@/components/media-editor/bridge-plugin';

export const PLUGINS = [
  {
    ...plugin_base_backstage_FontLoader,
    sourceType: 'base'
  },
  {
    ...plugin_base_backstage_TimeTraveler,
    sourceType: 'base'
  },
  {
    ...plugin_base_drawers_AIToolsDrawer,
    sourceType: 'base'
  },
  {
    ...plugin_base_drawers_AdjustmentDrawer,
    sourceType: 'base'
  },
  {
    ...plugin_base_drawers_CraftDrawer,
    sourceType: 'base'
  },
  {
    ...plugin_base_drawers_ImageInfoDrawer,
    sourceType: 'base'
  },
  {
    ...plugin_base_drawers_LayersDrawer,
    sourceType: 'base'
  },
  {
    ...plugin_base_options_ClipOptions,
    sourceType: 'base'
  },
  {
    ...plugin_base_options_ColorOptions,
    sourceType: 'base'
  },
  {
    ...plugin_base_options_ViewportOptions,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_BrushOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_ClipOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_EraserOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_FileLoader,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_LayerOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_MarkerOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_MosaicOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_PixelGridOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_SmartGuides,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_TabDock,
    sourceType: 'base'
  },
  {
    ...plugin_base_overlays_TextOverlay,
    sourceType: 'base'
  },
  {
    ...plugin_base_panels_SettingsPanel,
    sourceType: 'base'
  },
  {
    ...plugin_base_panels_UserGuidePanel,
    sourceType: 'base'
  },
  {
    ...plugin_staqly_bridge,
    sourceType: 'community'
  }
];

// Vector-source contributor wiring: every plugin declaring
// contributions.engine.vectorSource is registered here, so importing this
// registry is sufficient to have all contributors in place before the first
// scene assembly (production, engine tests, and headless pipelines alike).
for (const p of PLUGINS) {
  const contributor = p.contributions?.engine?.vectorSource;
  if (contributor) {
    registerVectorSourceContributor(contributor);
  }
}

export default PLUGINS;
