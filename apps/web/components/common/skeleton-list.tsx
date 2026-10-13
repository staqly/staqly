import { Skeleton } from "@/components/ui/skeleton";

/** `count` identical loading placeholders. */
export function SkeletonList({ count, className }: { count: number; className?: string }) {
  return Array.from({ length: count }, (_, i) => <Skeleton key={i} className={className} />);
}
