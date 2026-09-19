import { useEffect, useState } from "react";
import { ogcExceptionMessage } from "@/lib/ogc-exception";

/** Keep the last good portrayal visible while a new render loads or fails. */
export function WMSPreview({
  url,
  revision,
  alt,
}: {
  url: string;
  revision: number;
  alt: string;
}) {
  const [image, setImage] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    // Effect-owned asynchronous work; do not drop the previous good image.
    void (async () => {
      setLoading(true);
      setError("");
      try {
        const response = await fetch(url, {
          credentials: "same-origin",
          cache: "no-store",
          signal: controller.signal,
        });
        if (
          !response.ok ||
          !response.headers.get("content-type")?.startsWith("image/")
        ) {
          throw new Error(
            ogcExceptionMessage(await response.text()).slice(0, 1200) ||
              `Preview failed (${response.status})`,
          );
        }
        const blob = await response.blob();
        if (!controller.signal.aborted) setImage(URL.createObjectURL(blob));
      } catch (reason) {
        if (!controller.signal.aborted)
          setError(reason instanceof Error ? reason.message : "Preview failed");
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    })();
    return () => controller.abort();
  }, [url, revision]);
  useEffect(
    () => () => {
      if (image) URL.revokeObjectURL(image);
    },
    [image],
  );
  return (
    <div className="space-y-2" aria-busy={loading}>
      {image && (
        <img
          src={image}
          alt={alt}
          className={`max-h-full max-w-full border ${loading || error ? "opacity-50" : ""}`}
        />
      )}
      {loading && (
        <p role="status" className="text-sm">
          Rendering preview…
        </p>
      )}
      {error && (
        <p
          role="alert"
          className="max-w-xl whitespace-pre-wrap break-words text-sm text-destructive"
        >
          {error}
        </p>
      )}
    </div>
  );
}
