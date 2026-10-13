import { cn } from "@/lib/utils";
import { CopyButton } from "./copy-button";

export function CodeBlock({
  code,
  title,
  className,
  language,
}: {
  code: string;
  title?: string;
  className?: string;
  language?: string;
}) {
  return (
    <div className={cn("group/code relative overflow-hidden rounded-lg border bg-muted/40", className)}>
      {(title || language) && (
        <div className="flex h-8 items-center justify-between border-b bg-muted/50 px-3 text-xs text-muted-foreground">
          <span className="font-medium">{title}</span>
          {language && <span className="font-mono uppercase tracking-wide opacity-70">{language}</span>}
        </div>
      )}
      <pre className="scrollbar-thin overflow-x-auto p-3 pr-12 font-mono text-[12.5px] leading-relaxed">
        <code>{code}</code>
      </pre>
      <CopyButton
        value={code}
        label={title ? `Copy ${title}` : "Copy code"}
        className={cn("absolute right-1.5 bg-background/70 backdrop-blur", title || language ? "top-9" : "top-1.5")}
      />
    </div>
  );
}

export function InlineCode({ children, className }: { children: React.ReactNode; className?: string }) {
  return <code className={cn("rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]", className)}>{children}</code>;
}
