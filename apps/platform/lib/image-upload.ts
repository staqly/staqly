/** The side, in px, of an uploaded logo or picture: crisp at 2x in the 32-48 px places it shows. */
const SIDE = 128
/** The API takes 64 KB; JPEG when a PNG of a photo would be larger. */
const MAX_BYTES = 60_000

/**
 * Reads an image file the person picked (any format the browser decodes) and returns it as a square PNG or JPEG
 * data: URL, SIDE px, cropped to the centre: what `PATCH /v1/workspace` (logo) and `PATCH /v1/account` (avatar)
 * accept.
 */
export async function imageToDataUrl(file: File): Promise<string> {
  const bitmap = await createImageBitmap(file)
  const side = Math.min(bitmap.width, bitmap.height)
  const canvas = document.createElement('canvas')

  canvas.width = SIDE
  canvas.height = SIDE
  canvas
    .getContext('2d')!
    .drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, SIDE, SIDE)
  bitmap.close()

  const png = canvas.toDataURL('image/png')

  return png.length * 0.75 <= MAX_BYTES ? png : canvas.toDataURL('image/jpeg', 0.85)
}
