import type { Metadata } from "next";
import { N8nPage } from "@/components/integrations/pages";

export const metadata: Metadata = { title: "n8n" };

export default function Page() {
  return <N8nPage />;
}
