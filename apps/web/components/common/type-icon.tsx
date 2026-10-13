import { Braces, File, FileText, ImageIcon, Link2, Video } from "lucide-react";
import type { LucideProps } from "lucide-react";
import type { ItemType } from "@/lib/api";

const ICONS: Record<ItemType, React.ComponentType<LucideProps>> = {
  image: ImageIcon,
  video: Video,
  text: FileText,
  link: Link2,
  json: Braces,
  file: File,
};

export const TYPE_LABELS: Record<ItemType, string> = {
  image: "Image",
  video: "Video",
  text: "Text",
  link: "Link",
  json: "JSON",
  file: "File",
};

export function TypeIcon({ type, ...props }: { type: string } & LucideProps) {
  const Icon = ICONS[type as ItemType] ?? File;
  return <Icon aria-label={type} {...props} />;
}
