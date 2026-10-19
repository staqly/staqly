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

'use client';

import { useEffect } from 'react';
import { PLUGINS as CORE_PLUGINS } from '@opengpex/editor/core/plugin/registry';
import { EditorPlugin, BuiltPlugin, EditorCommand, BuiltCommand, EditorActions, EditorSignal, PluginManifest } from '@opengpex/editor/core/types';
import { CORE_VERSION, satisfiesCoreVersion } from '@opengpex/editor/core/plugin/version';

/**
 * usePluginInit: Plugin system initialization
 * Manages the lifecycle of the plugin system, hotkeys, and global services.
 * Completely decoupled from UI.
 */
export function usePluginInit(actions: EditorActions) {
    useEffect(() => {
        // Safe Mode check: if ?safe-mode=true is in the URL, only load official core plugins
        const isSafeMode = typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('safe-mode') === 'true';

        // In Safe Mode, we load all official core plugins (base + community) but skip user/dynamic plugins
        const activeCorePlugins = CORE_PLUGINS as EditorPlugin[];

        if (isSafeMode) {
            console.warn('⚠️ [OpenGPEX Safe Mode] Safe mode is active. Only official core plugins are loaded.');
        }

        // Load user-enabled plugins map from localStorage
        let enabledUserPlugins: Record<string, boolean> = {};
        try {
            const stored = typeof localStorage !== 'undefined' ? localStorage.getItem('gpex_enabled_user_plugins') : null;
            if (stored) enabledUserPlugins = JSON.parse(stored);
        } catch { }

        // Helper to calculate UID at runtime
        const getPluginUid = (manifest: Partial<PluginManifest> | undefined, folderName: string): string => {
            const rawAuthor = manifest?.author || 'anonymous';
            const author = rawAuthor.toLowerCase().replace(/[^a-z0-9_-]/g, '_');
            const manifestId = manifest?.id || folderName;
            return `${author}.${manifestId}`;
        };

        // Helper to calculate Group prefix at runtime
        const getPluginGroup = (sourceType?: string): string => {
            return `gpex.plugins.${sourceType || 'user'}.`;
        };

        // Define a helper function to register a single plugin
        const registerSinglePlugin = (rawPlugin: EditorPlugin) => {
            if (!rawPlugin) {
                console.warn(`[PluginInit] Plugin is undefined.`);
                return;
            }

            if (!rawPlugin.manifest?.id || !rawPlugin.manifest?.author) {
                console.error(
                    `❌ [PluginInit] Plugin in folder "${rawPlugin._folderName || 'unknown'}" is missing required manifest fields 'id' or 'author'. ` +
                    `Registration skipped.`
                );
                return;
            }

            const folderName = rawPlugin._folderName || '';
            const uid = getPluginUid(rawPlugin.manifest, folderName);
            const group = getPluginGroup(rawPlugin.sourceType);

            // 🛡️ Version Gate: Verify if plugin's coreVersion requirement is met by current kernel version
            const requiredCore = rawPlugin.manifest?.requirements?.coreVersion;
            if (requiredCore && !satisfiesCoreVersion(requiredCore)) {
                console.warn(
                    `[PluginInit] Plugin "${uid}" requires core ${requiredCore}, ` +
                    `but current core is ${CORE_VERSION}. Plugin will not be loaded.`
                );
                return;
            }

            // User plugins default to DISABLED — strictly check standard uid
            const isEnabled = rawPlugin.sourceType === 'user'
                ? !!enabledUserPlugins[uid]
                : true;

            const sanitizedPlugin: BuiltPlugin = {
                ...rawPlugin,
                uid: uid,
                group: group,
                enabled: isEnabled,
                manifest: {
                    ...rawPlugin.manifest,
                    id: rawPlugin.manifest?.id || folderName,
                    displayName: rawPlugin.manifest?.displayName || folderName,
                    category: rawPlugin.manifest?.category || 'user',
                    author: rawPlugin.manifest?.author || 'anonymous'
                },
                commands: rawPlugin.commands?.map((cmd: EditorCommand) => ({
                    ...cmd,
                    uid: `${uid}.${cmd.id}`
                })),
                signals: rawPlugin.signals?.map((sig: EditorSignal) => ({
                    ...sig,
                    uid: `${uid}.${sig.id}`
                }))
            } as BuiltPlugin;

            try {
                actions.registerPlugin(sanitizedPlugin);

                // Invoke plugin lifecycle onInit hook (if defined and plugin is enabled)
                // Note: Full EditorContextValue is not available at registration time.
                // We provide a partial ctx with actions. Plugins that need full ctx
                // (e.g. scoped config) should guard with optional chaining.
                if (isEnabled && rawPlugin.onInit) {
                    try {
                        rawPlugin.onInit({ actions } as never);
                    } catch (err) {
                        console.warn(`[PluginInit] onInit failed for plugin "${uid}":`, err);
                    }
                }

                // Register commands and associate shortcuts only if enabled
                if (isEnabled) {
                    sanitizedPlugin.commands?.forEach((cmd: BuiltCommand) => {
                        actions.registerCommand(cmd);

                        const shortcutConfigs = cmd.shortcuts || (cmd.shortcut ? [cmd.shortcut] : []);
                        shortcutConfigs.forEach((sc, idx) => {
                            actions.registerShortcut({
                                id: shortcutConfigs.length > 1 ? `${cmd.uid}-${idx}` : cmd.uid,
                                name: cmd.name,
                                category: sanitizedPlugin.manifest?.category || 'General',
                                ...sc,
                                action: () => actions.executeCommand(cmd.uid, { _shortcutKey: sc.key }),
                                description: cmd.name
                            });
                        });
                    });
                }
            } catch (err) {
                console.error(`[PluginInit] Failed to initialize plugin: ${uid}`, err);
            }
        };

        // 1. First register CORE_PLUGINS (sequential order)
        activeCorePlugins.forEach((p) => registerSinglePlugin(p));

        // staqly: no user or downloaded plugins; the editor is the built-in set (core/plugin/registry.ts).
        // 3. Cleanup function: unregister all plugins when component unmounts (cascading cleanup for commands/shortcuts/signals)
        return () => {
            activeCorePlugins.forEach((p) => {
                const uid = getPluginUid(p.manifest, p.manifest.id);
                actions.unregisterPlugin(uid);
            });
        };
    }, [actions]);
}
