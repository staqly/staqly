"use client";

import { Download, ExternalLink, FileIcon, ImageOff } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { TypeIcon } from "@/components/common/type-icon";
import { mediaUrl, type Item } from "@/lib/api";
import { formatBytes, hostOf, prettyJSON, truncate } from "@/lib/format";
import { cn } from "@/lib/utils";

/** URL to render for image/video items: uploaded media first, else the external URL. */
export function itemMediaSrc(item: Item): string | null {
  if (item.media?.id) return mediaUrl(item.media.id);
  if ((item.type === "image" || item.type === "video") && item.url) return item.url;
  return null;
}

function isVideoMime(item: Item) {
  return item.media?.mime_type?.startsWith("video/") ?? false;
}

function BrokenMedia({ label }: { label: string }) {
  return (
    <div className="flex h-full min-h-40 w-full flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      <ImageOff className="size-6" />
      {label}
    </div>
  );
}

interface PreviewProps {
  item: Item;
  className?: string;
  compact?: boolean;
}

export function ItemPreview(props: PreviewProps) {
  return <ItemPreviewInner key={`${props.item.id}:${props.item.media?.id ?? props.item.url}`} {...props} />;
}

function ItemPreviewInner({ item, className, compact = false }: PreviewProps) {
  const src = itemMediaSrc(item);
  const [broken, setBroken] = useState(false);

  if (item.type === "image" && src) {
    return (
      <div
        data-testid="preview-image"
        className={cn("bg-checker flex items-center justify-center overflow-hidden rounded-lg border", className)}
      >
        {broken ? (
          <BrokenMedia label="Image could not be loaded" />
        ) : (
          // oxlint-disable-next-line nextjs/no-img-element -- authenticated media served via the API proxy
          <img
            src={src}
            alt={item.title || "Image"}
            onError={() => setBroken(true)}
            className={cn("max-h-full max-w-full object-contain", compact ? "max-h-[50vh]" : "max-h-[70vh]")}
          />
        )}
      </div>
    );
  }

  if ((item.type === "video" || isVideoMime(item)) && src) {
    return (
      <div data-testid="preview-video" className={cn("overflow-hidden rounded-lg border bg-black", className)}>
        {/* oxlint-disable-next-line jsx-a11y/media-has-caption -- user-uploaded media; staq.ly has no caption tracks to offer */}
        <video
          key={src}
          src={src}
          controls
          playsInline
          preload="metadata"
          className={cn("mx-auto w-full", compact ? "max-h-[50vh]" : "max-h-[70vh]")}
        />
      </div>
    );
  }

  if (item.type === "json") {
    return (
      <pre
        data-testid="preview-json"
        className={cn(
          "scrollbar-thin overflow-auto rounded-lg border bg-muted/40 p-4 font-mono text-[12.5px] leading-relaxed",
          compact ? "max-h-[50vh]" : "max-h-[70vh]",
          className,
        )}
      >
        {item.content ? prettyJSON(item.content) : "null"}
      </pre>
    );
  }

  if (item.type === "link") {
    const href = item.url || item.content;
    return (
      <div data-testid="preview-link" className={cn("rounded-lg border bg-card p-4", className)}>
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
            <TypeIcon type="link" className="size-5" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              {href ? hostOf(href) : "No URL"}
            </div>
            <div className="mt-0.5 font-medium">{item.title || href}</div>
            {href && (
              <a
                href={href}
                target="_blank"
                rel="noopener noreferrer nofollow"
                className="mt-1 block truncate font-mono text-xs text-brand hover:underline"
              >
                {href}
              </a>
            )}
            {item.content && item.content !== href && (
              <p className="mt-3 text-sm whitespace-pre-wrap text-muted-foreground">{item.content}</p>
            )}
          </div>
          {href && (
            <Button asChild variant="outline" size="sm">
              <a href={href} target="_blank" rel="noopener noreferrer nofollow">
                <ExternalLink /> Open
              </a>
            </Button>
          )}
        </div>
      </div>
    );
  }

  if (item.type === "file" || (item.media && !src)) {
    const m = item.media;
    return (
      <div
        data-testid="preview-file"
        className={cn("flex items-center gap-3 rounded-lg border bg-card p-4", className)}
      >
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
          <FileIcon className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium">{m?.original_name || item.title || "File"}</div>
          <div className="text-xs text-muted-foreground">
            {m ? `${m.mime_type} · ${formatBytes(m.size_bytes)}` : item.url ? hostOf(item.url) : "No file attached"}
          </div>
          {item.content && <p className="mt-2 text-sm whitespace-pre-wrap text-muted-foreground">{item.content}</p>}
        </div>
        {m ? (
          <Button asChild variant="outline" size="sm">
            <a href={mediaUrl(m.id, { download: true })} download={m.original_name}>
              <Download /> Download
            </a>
          </Button>
        ) : item.url ? (
          <Button asChild variant="outline" size="sm">
            <a href={item.url} target="_blank" rel="noopener noreferrer nofollow">
              <ExternalLink /> Open
            </a>
          </Button>
        ) : null}
      </div>
    );
  }

  // text (and image/video items missing a source)
  if ((item.type === "image" || item.type === "video") && !src) {
    return (
      <div className={cn("rounded-lg border bg-muted/30", className)}>
        <BrokenMedia label={`No ${item.type} attached`} />
      </div>
    );
  }

  return (
    <div
      data-testid="preview-text"
      className={cn(
        "scrollbar-thin overflow-auto rounded-lg border bg-card p-5 text-[14px] leading-relaxed whitespace-pre-wrap",
        compact ? "max-h-[50vh]" : "max-h-[70vh]",
        className,
      )}
    >
      {item.content || <span className="text-muted-foreground italic">Empty</span>}
    </div>
  );
}

/** Small square thumbnail for lists, tables and the inbox. */
export function ItemThumb(props: { item: Item; className?: string }) {
  return <ItemThumbInner key={`${props.item.id}:${props.item.media?.id ?? props.item.url}`} {...props} />;
}

function ItemThumbInner({ item, className }: { item: Item; className?: string }) {
  const src = itemMediaSrc(item);
  const [broken, setBroken] = useState(false);
  const base = cn(
    "relative flex shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted",
    className,
  );
  if (src && !broken && item.type === "image") {
    return (
      <div className={base}>
        {/* oxlint-disable-next-line nextjs/no-img-element -- authenticated media served via the API proxy */}
        <img src={src} alt="" loading="lazy" className="size-full object-cover" onError={() => setBroken(true)} />
      </div>
    );
  }
  if (src && !broken && (item.type === "video" || isVideoMime(item))) {
    return (
      <div className={base}>
        <video
          src={`${src}#t=0.1`}
          muted
          playsInline
          preload="metadata"
          className="size-full object-cover"
          onError={() => setBroken(true)}
        />
        <span className="absolute right-0.5 bottom-0.5 rounded bg-black/60 p-0.5 text-white">
          <TypeIcon type="video" className="size-2.5" />
        </span>
      </div>
    );
  }
  return (
    <div className={cn(base, "text-muted-foreground")}>
      <TypeIcon type={item.type} className="size-4" />
    </div>
  );
}

/** Text excerpt for cards when there is no visual media. */
export function ItemExcerpt({ item, className, length = 220 }: { item: Item; className?: string; length?: number }) {
  if (item.type === "json") {
    return (
      <pre className={cn("overflow-hidden font-mono text-[11px] leading-snug text-muted-foreground", className)}>
        {truncate(prettyJSON(item.content), length)}
      </pre>
    );
  }
  if (item.type === "link") {
    const href = item.url || item.content;
    return (
      <div className={cn("text-xs text-muted-foreground", className)}>
        <div className="truncate font-mono text-brand">{href ? hostOf(href) : ""}</div>
        {item.content && item.content !== href && <p className="mt-1 line-clamp-4">{item.content}</p>}
      </div>
    );
  }
  if (item.type === "file") {
    return (
      <div className={cn("text-xs text-muted-foreground", className)}>
        {item.media ? `${item.media.original_name} · ${formatBytes(item.media.size_bytes)}` : item.url}
      </div>
    );
  }
  return (
    <p className={cn("text-xs leading-relaxed whitespace-pre-wrap text-muted-foreground", className)}>
      {truncate(item.content || "", length)}
    </p>
  );
}
