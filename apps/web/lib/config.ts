/** Public base URL of the API as seen by agents/tools on the host machine. */
export const PUBLIC_API_URL = (process.env.NEXT_PUBLIC_API_URL || "http://localhost:3333").replace(/\/+$/, "");
export const API_URL_FROM_ENV = !!process.env.NEXT_PUBLIC_API_URL;
