import type { Metadata } from "next";
import { McpPage } from "@/components/integrations/pages";

export const metadata: Metadata = { title: "MCP" };

export default function Page() {
  return <McpPage />;
}
