# Brand assets used by the product

`staqly-logo-readme.svg` is used by the root README. App icons, marks and social cards live in each app's
`app/` and `public/brand/` directories. Shared components, CSS tokens and fonts remain in `packages/brand`.

Original artwork, alternate exports, brand guidelines and marketing banners live in the separate staqly
assets repository (`brand/`, in a sibling checkout at `../assets` from the monorepo root).
When artwork changes, update its source there and copy the needed exports into this repository.
Product builds use only the local copies and do not require the assets repository.
