import type { Metadata } from "next";
import { Suspense } from "react";
import { ApiKeysManager } from "@/components/integrations/api-keys";

export const metadata: Metadata = { title: "API keys" };

export default function Page() {
  return (
    <Suspense>
      <ApiKeysManager />
    </Suspense>
  );
}
