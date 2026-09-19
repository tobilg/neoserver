import * as maplibre from "maplibre-gl";
import workerURL from "maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url";

// Resolve the worker as an asset in both Vite development and embedded builds.
maplibre.setWorkerUrl(workerURL);
export { maplibre };
