"use client";
import { createOpenAPIPage } from "fumadocs-openapi/ui";

// The interactive playground is disabled: the docs are hosted separately from
// any staq.ly instance, and the API does not accept cross-origin browser calls.
export const OpenAPIPage = createOpenAPIPage({
  playground: { enabled: false },
});
