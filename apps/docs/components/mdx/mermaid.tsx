"use client";

import { use, useId, useSyncExternalStore } from "react";
import { useTheme } from "next-themes";

/**
 * Client-side Mermaid renderer (works with static export: nothing is rendered
 * at build time). Themed with the staq.ly graphite + ember palette.
 */
export function Mermaid({ chart }: { chart: string }) {
  const isClient = useSyncExternalStore(
    () => () => {},
    () => true,
    () => false,
  );
  if (!isClient) return <div className="sq-mermaid sq-mermaid-placeholder" aria-hidden />;
  return <MermaidContent chart={chart} />;
}

const cache = new Map<string, Promise<unknown>>();

function cachePromise<T>(key: string, create: () => Promise<T>): Promise<T> {
  const hit = cache.get(key);
  if (hit) return hit as Promise<T>;
  const p = create();
  cache.set(key, p);
  return p;
}

const light = {
  background: "transparent",
  primaryColor: "#fff4ec",
  primaryBorderColor: "#f2622b",
  primaryTextColor: "#25262c",
  secondaryColor: "#f3f4f6",
  tertiaryColor: "#f7f7f9",
  lineColor: "#8b8e98",
  textColor: "#25262c",
  edgeLabelBackground: "#ffffff",
  clusterBkg: "#fafafb",
  clusterBorder: "#e4e5ea",
};

const dark = {
  background: "transparent",
  primaryColor: "#2a211d",
  primaryBorderColor: "#f58a4b",
  primaryTextColor: "#eceef2",
  secondaryColor: "#25272d",
  tertiaryColor: "#1f2126",
  lineColor: "#7d818c",
  textColor: "#eceef2",
  edgeLabelBackground: "#1d1f24",
  clusterBkg: "#1d1f24",
  clusterBorder: "#34363d",
};

function MermaidContent({ chart }: { chart: string }) {
  const id = useId().replace(/[^a-zA-Z0-9]/g, "");
  const { resolvedTheme } = useTheme();
  const isDark = resolvedTheme === "dark";
  const { default: mermaid } = use(cachePromise("mermaid", () => import("mermaid")));

  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    fontFamily: "var(--font-geist-sans), ui-sans-serif, system-ui, sans-serif",
    theme: "base",
    themeVariables: { ...(isDark ? dark : light), fontSize: "14px" },
    flowchart: { curve: "basis", padding: 12, htmlLabels: true },
  });

  const { svg } = use(
    cachePromise(`${chart}-${isDark ? "dark" : "light"}`, () =>
      mermaid.render(`m${id}${isDark ? "d" : "l"}`, chart.replaceAll("\\n", "\n")),
    ),
  );

  return <div className="sq-mermaid" dangerouslySetInnerHTML={{ __html: svg }} />;
}
