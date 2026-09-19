import { cn } from "@/lib/utils";

export interface ZoomProgress {
  zoom: number;
  total_tiles: number;
  processed_tiles: number;
}
export function PyramidMeter({ levels }: { levels: ZoomProgress[] }) {
  const processed = levels.reduce(
    (sum, level) => sum + level.processed_tiles,
    0,
  );
  const total = levels.reduce((sum, level) => sum + level.total_tiles, 0);
  const percentage = total ? Math.round((processed / total) * 100) : 0;
  return (
    <div
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percentage}
      aria-label="Tile pyramid progress"
      className="space-y-1"
    >
      {levels.map((level, index) => {
        const value = level.total_tiles
          ? level.processed_tiles / level.total_tiles
          : 0;
        return (
          <div key={level.zoom} className="flex items-center gap-2">
            <span className="w-7 text-right font-mono text-xs text-muted-foreground">
              z{level.zoom}
            </span>
            <div
              className="h-1.5 rounded-full bg-muted"
              style={{
                width: `${Math.max(18, ((index + 1) / levels.length) * 100)}%`,
              }}
            >
              <div
                className={cn(
                  "h-full rounded-full transition-[width] duration-300",
                  value >= 1
                    ? "bg-success"
                    : value > 0
                      ? "bg-brand"
                      : "bg-transparent",
                )}
                style={{ width: `${value * 100}%` }}
              />
            </div>
            <span className="ml-auto font-mono text-[11px] text-muted-foreground tabular-nums">
              {level.processed_tiles}/{level.total_tiles}
            </span>
          </div>
        );
      })}
      <span className="sr-only">{percentage}% complete</span>
    </div>
  );
}
