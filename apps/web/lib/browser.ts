/** Full page navigation (drops every in-memory cache and secret). Wrapped so tests can spy on it. */
export const browser = {
  assign(href: string) {
    window.location.assign(href);
  },
};
