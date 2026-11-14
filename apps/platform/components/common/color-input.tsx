/** A colour field: the browser's own picker in a swatch. */
export function ColorInput({
  value,
  onChange,
  label,
}: {
  value: string
  onChange: (c: string) => void
  label: string
}) {
  return (
    <label
      className="relative flex size-8 shrink-0 cursor-pointer items-center justify-center overflow-hidden rounded-md border shadow-xs pointer-coarse:size-11"
      style={{ background: value }}
      title={label}
    >
      <span className="sr-only">{label}</span>
      <input
        type="color"
        value={value.slice(0, 7)}
        onChange={(e) => onChange(e.target.value)}
        className="absolute inset-0 cursor-pointer opacity-0"
      />
    </label>
  )
}
