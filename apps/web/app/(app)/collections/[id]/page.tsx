import type { Metadata } from "next";
import { Suspense } from "react";
import { CollectionPage } from "@/components/collections/collection-page";

export const metadata: Metadata = { title: "Collection" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CollectionPage id={decodeURIComponent(id)} />
    </Suspense>
  );
}
