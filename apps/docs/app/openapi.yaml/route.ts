import { readFile } from "node:fs/promises";
import { specPath } from "@/lib/openapi";

export const revalidate = false;

/** The raw OpenAPI contract, copied into the static export. */
export async function GET() {
  return new Response(await readFile(specPath, "utf8"), {
    headers: { "Content-Type": "application/yaml; charset=utf-8" },
  });
}
