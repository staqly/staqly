import type { Metadata } from "next";
import { Suspense } from "react";
import { ActivityFeed } from "@/components/activity/activity-feed";

export const metadata: Metadata = { title: "Activity" };

export default function ActivityPage() {
  return (
    <Suspense>
      <ActivityFeed />
    </Suspense>
  );
}
