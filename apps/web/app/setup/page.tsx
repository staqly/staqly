import type { Metadata } from "next";
import { SetupFlow } from "@/components/auth/setup-flow";

export const metadata: Metadata = { title: "Set up your workspace" };

export default function SetupPage() {
  return <SetupFlow />;
}
