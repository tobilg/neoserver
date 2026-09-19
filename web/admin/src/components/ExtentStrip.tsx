import type { SpatialExtent } from "@/api/generated/models";

function projectX(value: number) {
  return ((value + 180) / 360) * 88;
}
function projectY(value: number) {
  return ((90 - value) / 180) * 44;
}

export function ExtentStrip({ extent }: { extent?: SpatialExtent }) {
  if (!extent || extent.srid !== 4326)
    return <span className="text-xs text-muted-foreground">Not reported</span>;
  const x = Math.max(0, projectX(extent.min_x));
  const y = Math.max(0, projectY(extent.max_y));
  const width = Math.min(88, projectX(extent.max_x)) - x;
  const height = Math.min(44, projectY(extent.min_y)) - y;
  const label = `extent: ${extent.min_x} to ${extent.max_x} longitude, ${extent.min_y} to ${extent.max_y} latitude`;
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox="0 0 88 44"
      className="h-11 w-[88px] rounded-md border bg-muted/50"
    >
      <title>{label}</title>
      <path
        d="M4 13l9-7 11 4 6 8-4 8-10 2-7-5zm29-5 12-5 9 4 2 6 10 2 14 9-4 8-17 2-8-6-8 3-7-7z"
        fill="none"
        stroke="currentColor"
        strokeWidth=".7"
        className="text-muted-foreground/50"
      />
      <rect
        x={x}
        y={y}
        width={Math.max(1, width)}
        height={Math.max(1, height)}
        className="fill-brand/20 stroke-brand"
        strokeWidth="1"
        rx="1"
      />
      {width < 3 && height < 3 && (
        // A tiny extent would be invisible at this scale; ring its location.
        <circle
          data-marker
          cx={x + Math.max(1, width) / 2}
          cy={y + Math.max(1, height) / 2}
          r="4"
          fill="none"
          className="stroke-brand"
          strokeWidth="1.2"
        />
      )}
    </svg>
  );
}
