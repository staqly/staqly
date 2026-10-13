import type { Metadata } from "next";
import { ChannelsPage } from "@/components/notifications/channels-page";

export const metadata: Metadata = { title: "Notifications" };

export default function Page() {
  return <ChannelsPage />;
}
