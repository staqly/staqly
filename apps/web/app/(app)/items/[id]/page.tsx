import type { Metadata } from "next";
import { PageContainer } from "@/components/common/page-header";
import { ItemDetail } from "@/components/items/item-detail";

export const metadata: Metadata = { title: "Item" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <PageContainer className="max-w-7xl">
      <ItemDetail id={decodeURIComponent(id)} />
    </PageContainer>
  );
}
