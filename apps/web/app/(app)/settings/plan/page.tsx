import type { Metadata } from "next";
import { PlanUsagePage } from "@/components/settings/plan-usage-page";

export const metadata: Metadata = { title: "Plan & usage" };

export default function Page() {
  return <PlanUsagePage />;
}
