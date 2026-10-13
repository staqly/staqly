import Link from "next/link";
import { Button } from "@/components/ui/button";
import { LogoMark } from "@/components/common/logo";

export default function NotFound() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 p-6 text-center">
      <LogoMark className="size-10" />
      <div>
        <h1 className="text-xl font-semibold">Page not found</h1>
        <p className="mt-1 text-sm text-muted-foreground">There is nothing stashed at this address.</p>
      </div>
      <Button asChild variant="outline">
        <Link href="/inbox">Back to inbox</Link>
      </Button>
    </div>
  );
}
