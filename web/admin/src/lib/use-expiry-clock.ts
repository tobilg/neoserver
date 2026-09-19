import { useEffect, useState } from "react";

/** Update at expiration, and after background tabs resume; never require reload. */
export function useExpiryClock(expirations: (string | undefined)[]) {
  const [now, setNow] = useState(Date.now);
  const next = Math.min(
    ...expirations
      .map((date) => (date ? Date.parse(date) : Infinity))
      .filter((time) => time > now),
  );
  useEffect(() => {
    const tick = () => setNow(Date.now());
    const timer = Number.isFinite(next)
      ? window.setTimeout(
          tick,
          Math.max(1, Math.min(next - Date.now(), 2_147_483_647)),
        )
      : undefined;
    window.addEventListener("focus", tick);
    document.addEventListener("visibilitychange", tick);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener("focus", tick);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [next, now]);
  return now;
}
