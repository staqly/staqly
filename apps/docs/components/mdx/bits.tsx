import type { ReactNode } from "react";

/** Small inline label, e.g. <Badge>cloud</Badge>. */
export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: "neutral" | "brand" | "success" | "warn";
}) {
  return <span className={`sq-badge sq-badge-${tone}`}>{children}</span>;
}

/** Keyboard key, e.g. <Kbd>j</Kbd>. */
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="sq-kbd">{children}</kbd>;
}

/** HTTP method + path, e.g. <Method m="POST" path="/v1/rules" />. */
export function Method({ m, path }: { m: "GET" | "POST" | "PATCH" | "DELETE" | "PUT"; path: string }) {
  return (
    <span className="sq-endpoint">
      <span className={`sq-method sq-method-${m.toLowerCase()}`}>{m}</span>
      <code>{path}</code>
    </span>
  );
}

/** Banner for features that are planned but not available yet. */
export function ComingSoon({ children }: { children?: ReactNode }) {
  return (
    <div className="sq-coming-soon">
      <span className="sq-badge sq-badge-brand">Coming soon</span>
      <div>{children}</div>
    </div>
  );
}
