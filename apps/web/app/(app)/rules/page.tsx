import type { Metadata } from "next";
import { RulesPage } from "@/components/rules/rules-page";

export const metadata: Metadata = { title: "Rules" };

export default function Page() {
  return <RulesPage />;
}
