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

"use client";

import { CORE_VERSION } from "@opengpex/editor/core/plugin/version";
import { useState, useEffect, useRef } from "react";
import {
  ImagePlus,
  Loader2,
  Upload,
  Sparkles,
  Layers,
  Scissors,
  Zap,
  RefreshCw,
  Trash2,
  AlertTriangle,
  SlidersHorizontal,
  FileImage,
  Cpu,
  Eraser,
  Expand,
  Target,
  ImageMinus,
} from "lucide-react";
import EditorHUD from "@opengpex/editor/widgets/EditorHUD";
import DelayedConfirm from "@opengpex/editor/widgets/DelayedConfirm";
import { FancyButton } from "@opengpex/editor/widgets/FancyButton";
import {
  usePluginCommands,
  useEditorState,
  useEditorServices,
} from "@opengpex/editor/core/context";
import type { FileLoaderCommandsMap } from './commands.d';
import type { ImportingSignalValue } from './protocols';
import { SIGNAL_IMPORTING } from './protocols';
import { GITHUB_REPO_URL } from "@opengpex/editor/core/helpers/config";

// ─── HUD Title/Subtitle Helpers ─────────────────────────────────────────────

/** Compute the HUD title based on importing signal progress. */
function getImportingTitle(signal: ImportingSignalValue | null): string {
  if (!signal) return '';
  const { current, total } = signal;
  const progress = total > 1 ? ` ${current}/${total}` : '';
  return `Loading${progress}…`;
}

/**
 * FileLoaderComponent: Flagship global drag-and-drop service component (main component)
 */
export function FileLoaderComponent() {
  const { importCmd } = usePluginCommands<FileLoaderCommandsMap>();
  const [isOver, setIsOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { state } = useEditorState();

  // Unified importing signal (replaces separate transcoding/downloading booleans)
  const importingSignal = state.interaction.signals[SIGNAL_IMPORTING] as unknown as ImportingSignalValue | null;
  const isImporting = !!importingSignal;

  // Track restoreTimedOut signal in a ref so it's accessible inside event listeners.
  // Must sync via useEffect (not during render) per react-hooks/refs rule.
  const restoreTimedOutRef = useRef(false);
  useEffect(() => {
    restoreTimedOutRef.current = !!state.getStateSignal("core.restoreTimedOut", false);
  });

  useEffect(() => {
    const onDragOver = (e: DragEvent) => {
      e.preventDefault();
      if (e.dataTransfer?.types.includes("Files")) setIsOver(true);
    };

    const onDragLeave = (e: DragEvent) => {
      if (e.relatedTarget === null) setIsOver(false);
    };

    const onDrop = (e: DragEvent) => {
      e.preventDefault();
      // Block file drop when restore timed out — user must choose retry/restart first
      if (restoreTimedOutRef.current) { setIsOver(false); return; }
      setIsOver(false);
      const files = e.dataTransfer?.files;
      if (files && files.length > 0) importCmd?.execute(Array.from(files));
    };

    const onTriggerPicker = () => fileInputRef.current?.click();

    window.addEventListener("dragover", onDragOver);
    window.addEventListener("dragleave", onDragLeave);
    window.addEventListener("drop", onDrop);
    window.addEventListener("editor:trigger-file-picker", onTriggerPicker);

    return () => {
      window.removeEventListener("dragover", onDragOver);
      window.removeEventListener("dragleave", onDragLeave);
      window.removeEventListener("drop", onDrop);
      window.removeEventListener("editor:trigger-file-picker", onTriggerPicker);
    };
  }, [importCmd]);

  return (
    <div
      className={`absolute inset-0 z-[100] pointer-events-none transition-all duration-300 ${isOver ? "bg-indigo-600/5 backdrop-blur-[2px]" : ""}`}
    >
      <input
        type="file"
        ref={fileInputRef}
        onChange={(e) => {
          if (e.target.files && e.target.files.length > 0) {
            importCmd?.execute(Array.from(e.target.files));
          }
          e.target.value = "";
        }}
        accept="image/*,.eps,.epsf,application/postscript"
        multiple
        className="hidden"
      />

      {/* Drag visual feedback */}
      <div
        className={`absolute inset-0 border-[6px] border-dashed border-indigo-500/20 transition-opacity duration-300 ${isOver ? "opacity-100" : "opacity-0"}`}
      />

      <div className="absolute inset-0 flex items-center justify-center p-4">
        <EditorHUD
          isVisible={isOver}
          title="Ready to Drop"
          subtitle="Release to add as new project"
          icon={
            <div className="w-5 h-5 bg-indigo-600 rounded-full flex items-center justify-center shadow-lg shadow-indigo-600/40 transform-gpu animate-pulse-lite">
              <ImagePlus size={11} className="text-white" strokeWidth={3} />
            </div>
          }
        />
        <EditorHUD
          isVisible={isImporting}
          title={getImportingTitle(importingSignal)}
          subtitle="Preparing image for workspace"
          icon={
            <div className="w-5 h-5 bg-amber-500 rounded-full flex items-center justify-center shadow-lg shadow-amber-500/40">
              <Loader2
                size={11}
                className="text-white animate-spin-lite"
                strokeWidth={3}
              />
            </div>
          }
        />
      </div>
    </div>
  );
}

/**
 * FileLoaderAction: Trigger button contributed to TOOL_BAR
 */
export function FileLoaderAction() {
  const { pickCmd } = usePluginCommands<FileLoaderCommandsMap>();

  return (
    <>
      <FancyButton
        onClick={() => pickCmd?.execute()}
        title={`Upload Image (${pickCmd?.shortcutLabel || ""})`}
        tooltipPosition="right"
        iconOnly
        shape="rect"
      >
        <ImagePlus size={18} />
      </FancyButton>
    </>
  );
}

/* ═══════════════════════════════════════════════════════════════════════════
   FileLoaderLandingAction: Creative start screen (Photoshop-inspired)
   Contributed to LANDING_PAGE slot — this is the main content users see
   when no project is open.
   ═══════════════════════════════════════════════════════════════════════════ */

/** Detect macOS for shortcut labels */
const isMac =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
const modKey = isMac ? "⌘" : "Ctrl";

/** Feature pill data */
const FEATURES = [
  { icon: Zap, label: "120fps", color: "text-amber-400" },
  { icon: Layers, label: "Layers & Blend", color: "text-indigo-400" },
  { icon: ImageMinus, label: "AI BG Remove", color: "text-purple-400" },
  { icon: Expand, label: "AI Upscale", color: "text-violet-400" },
  { icon: Target, label: "AI Segment", color: "text-fuchsia-400" },
  { icon: Eraser, label: "AI Erase", color: "text-rose-400" },
  { icon: SlidersHorizontal, label: "Filters", color: "text-pink-400" },
  { icon: Scissors, label: "Clip & Select", color: "text-emerald-400" },
  { icon: FileImage, label: "RAW/TIFF/16-bit", color: "text-sky-400" },
  { icon: Cpu, label: "ComfyUI Bridge", color: "text-orange-400" },
  { icon: Sparkles, label: "Plugins", color: "text-cyan-400" },
];

export function FileLoaderLandingAction() {
  const { pickCmd } = usePluginCommands<FileLoaderCommandsMap>();
  const { state } = useEditorState();
  const { actions, storage } = useEditorServices();
  const restoreTimedOut = !!state.getStateSignal("core.restoreTimedOut", false);

  // ─── Recovery Mode: restore timed out → show retry / start fresh ───
  if (restoreTimedOut) {
    return (
      <div className="w-full max-w-md space-y-8 text-center lg:text-left animate-[fadeInUp_0.6s_ease-out_both]">
        <h1 className="text-xl font-semibold text-[var(--text-main)]">The last session could not be restored</h1>

        {/* ─── Recovery Actions ─── */}
        <div className="space-y-3">
          {/* Retry: reload page to try restore again */}
          <button
            onClick={() => window.location.reload()}
            className="group relative w-full flex items-center gap-4 px-5 py-4 rounded-2xl
              bg-[var(--bg-panel)]/60 dark:bg-white/[0.04]
              border border-[var(--border-subtle)] dark:border-white/[0.08]
              hover:border-indigo-500/40 dark:hover:border-indigo-400/30
              hover:bg-[var(--bg-panel)] dark:hover:bg-white/[0.07]
              backdrop-blur-md transition-all duration-300 cursor-pointer
              shadow-sm hover:shadow-lg hover:shadow-indigo-500/5
              active:scale-[0.98]"
          >
            <div className="flex-shrink-0 w-10 h-10 rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center shadow-md shadow-indigo-500/20">
              <RefreshCw size={18} className="text-white" strokeWidth={2.5} />
            </div>
            <div className="flex-1 text-left min-w-0">
              <div className="text-sm font-bold text-[var(--text-main)] group-hover:text-indigo-500 dark:group-hover:text-indigo-400 transition-colors">
                Retry Loading
              </div>
              <div className="text-[10px] text-[var(--text-muted)] mt-0.5">
                Reload page and try to restore your workspace again
              </div>
            </div>
          </button>

          {/* Start Fresh: clear signal and allow empty workspace (double-click to confirm) */}
          <DelayedConfirm
            onConfirm={() => {
              actions.setStateSignal("core.restoreTimedOut", false);
              // Save empty state to IndexedDB to prevent stale data issues
              storage.save(state);
            }}
            roundedClassName="rounded-2xl"
            confirmClassName="bg-rose-500/10 dark:bg-rose-400/5"
            ringColor="text-rose-500"
          >
            <div
              className="group relative w-full flex items-center gap-4 px-5 py-4 rounded-2xl
                bg-[var(--bg-panel)]/60 dark:bg-white/[0.04]
                border border-dashed border-[var(--border-subtle)] dark:border-white/[0.06]
                hover:border-rose-500/40 dark:hover:border-rose-400/30
                hover:bg-[var(--bg-panel)] dark:hover:bg-white/[0.07]
                backdrop-blur-md transition-all duration-300 cursor-pointer"
            >
              <div className="flex-shrink-0 w-10 h-10 rounded-xl bg-[var(--bg-stage)] dark:bg-white/[0.04] border border-[var(--border-subtle)] dark:border-white/[0.06] flex items-center justify-center">
                <Trash2 size={18} className="text-rose-400" strokeWidth={1.5} />
              </div>
              <div className="flex-1 text-left min-w-0">
                <div className="text-sm font-bold text-[var(--text-muted)] group-hover:text-rose-400 transition-colors">
                  Start Fresh
                </div>
                <div className="text-[10px] text-[var(--text-muted)] mt-0.5">
                  Click twice to confirm — discard previous workspace
                </div>
              </div>
            </div>
          </DelayedConfirm>
        </div>

        <style>{`
          @keyframes fadeInUp {
            from { opacity: 0; transform: translateY(12px); }
            to { opacity: 1; transform: translateY(0); }
          }
        `}</style>
      </div>
    );
  }

  // ─── Normal Mode ───
  return (
    <div className="w-full max-w-md space-y-8 text-center lg:text-left animate-[fadeInUp_0.6s_ease-out_both]">
      {/* staqly: no OpenGPEX branding; staqly's header names the page. */}
      <h1 className="text-xl font-semibold text-[var(--text-main)]">Open a picture to edit</h1>

      {/* ─── Quick Actions ─── */}
      <div className="space-y-3">
        {/* Primary: Open File */}
        <button
          onClick={() => pickCmd?.execute()}
          className="group relative w-full flex items-center gap-4 px-5 py-4 rounded-2xl
            bg-[var(--bg-panel)]/60 dark:bg-white/[0.04]
            border border-[var(--border-subtle)] dark:border-white/[0.08]
            hover:border-indigo-500/40 dark:hover:border-indigo-400/30
            hover:bg-[var(--bg-panel)] dark:hover:bg-white/[0.07]
            backdrop-blur-md transition-all duration-300 cursor-pointer
            shadow-sm hover:shadow-lg hover:shadow-indigo-500/5
            active:scale-[0.98]"
        >
          {/* Icon */}
          <div className="flex-shrink-0 w-10 h-10 rounded-xl bg-gradient-to-br from-indigo-500 to-purple-600 flex items-center justify-center shadow-md shadow-indigo-500/20 group-hover:shadow-lg group-hover:shadow-indigo-500/30 transition-shadow">
            <Upload size={18} className="text-white" strokeWidth={2.5} />
          </div>
          {/* Text */}
          <div className="flex-1 text-left min-w-0">
            <div className="text-sm font-bold text-[var(--text-main)] group-hover:text-indigo-500 dark:group-hover:text-indigo-400 transition-colors">
              Open Image
            </div>
            <div className="text-[10px] text-[var(--text-muted)] mt-0.5">
              PNG, JPEG, WebP, GIF, SVG, GPEX
            </div>
          </div>
          {/* Shortcut badge */}
          <div className="flex-shrink-0 hidden sm:flex items-center gap-0.5">
            <kbd className="px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-[var(--bg-stage)] dark:bg-white/[0.06] text-[var(--text-muted)] border border-[var(--border-subtle)]">
              {modKey}
            </kbd>
            <span className="text-[9px] text-[var(--text-muted)]">+</span>
            <kbd className="px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-[var(--bg-stage)] dark:bg-white/[0.06] text-[var(--text-muted)] border border-[var(--border-subtle)]">
              O
            </kbd>
          </div>
          {/* Hover glow */}
          <div className="absolute inset-0 rounded-2xl bg-gradient-to-r from-indigo-500/0 to-purple-500/0 group-hover:from-indigo-500/[0.03] group-hover:to-purple-500/[0.03] transition-all duration-300 pointer-events-none" />
        </button>

        {/* Secondary: Drag & Drop */}
        <div
          className="relative w-full flex items-center gap-4 px-5 py-3.5 rounded-2xl
            border border-dashed border-[var(--border-subtle)] dark:border-white/[0.06]
            bg-transparent hover:bg-[var(--bg-panel)]/30 dark:hover:bg-white/[0.02]
            transition-all duration-300"
        >
          <div className="flex-shrink-0 w-10 h-10 rounded-xl bg-[var(--bg-stage)] dark:bg-white/[0.04] border border-[var(--border-subtle)] dark:border-white/[0.06] flex items-center justify-center">
            <ImagePlus
              size={18}
              className="text-[var(--text-muted)]"
              strokeWidth={1.5}
            />
          </div>
          <div className="flex-1 text-left min-w-0">
            <div className="text-sm font-semibold text-[var(--text-muted)]">
              Drag & Drop
            </div>
            <div className="text-[10px] text-[var(--text-muted)]/60 mt-0.5">
              Drop any image file onto the workspace
            </div>
          </div>
        </div>

        {/* Tertiary: Paste from Clipboard */}
        <div
          className="relative w-full flex items-center gap-4 px-5 py-3.5 rounded-2xl
            border border-dashed border-[var(--border-subtle)] dark:border-white/[0.06]
            bg-transparent hover:bg-[var(--bg-panel)]/30 dark:hover:bg-white/[0.02]
            transition-all duration-300"
        >
          <div className="flex-shrink-0 w-10 h-10 rounded-xl bg-[var(--bg-stage)] dark:bg-white/[0.04] border border-[var(--border-subtle)] dark:border-white/[0.06] flex items-center justify-center">
            <Upload
              size={18}
              className="text-[var(--text-muted)]"
              strokeWidth={1.5}
            />
          </div>
          <div className="flex-1 text-left min-w-0">
            <div className="text-sm font-semibold text-[var(--text-muted)]">
              Paste from Clipboard
            </div>
            <div className="text-[10px] text-[var(--text-muted)]/60 mt-0.5">
              Paste an image directly from clipboard
            </div>
          </div>
          {/* Shortcut badge */}
          <div className="flex-shrink-0 hidden sm:flex items-center gap-0.5">
            <kbd className="px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-[var(--bg-stage)] dark:bg-white/[0.06] text-[var(--text-muted)] border border-[var(--border-subtle)]">
              {modKey}
            </kbd>
            <span className="text-[9px] text-[var(--text-muted)]">+</span>
            <kbd className="px-1.5 py-0.5 rounded text-[9px] font-mono font-bold bg-[var(--bg-stage)] dark:bg-white/[0.06] text-[var(--text-muted)] border border-[var(--border-subtle)]">
              V
            </kbd>
          </div>
        </div>
      </div>

      {/* ─── Animation keyframes ─── */}
      <style>{`
        @keyframes fadeInUp {
          from { opacity: 0; transform: translateY(12px); }
          to { opacity: 1; transform: translateY(0); }
        }
      `}</style>
    </div>
  );
}
