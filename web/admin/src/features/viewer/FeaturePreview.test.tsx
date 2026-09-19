import { act, cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderScreen } from "@/test/render";
import { FeaturePreview } from "./FeaturePreview";
import { useState } from "react";
import { fireEvent } from "@testing-library/react";

const maps = vi.hoisted(() => ({
  instances: [] as {
    callbacks: Record<string, (event?: unknown) => void>;
    visible: unknown[];
    remove: ReturnType<typeof vi.fn>;
  }[],
}));
vi.mock("@/lib/maplibre", () => ({
  maplibre: {
    Map: class {
      callbacks: Record<string, (event?: unknown) => void> = {};
      visible: unknown[] = [];
      remove = vi.fn();
      constructor() {
        maps.instances.push(this);
      }
      on(event: string, callback: (event?: unknown) => void) {
        this.callbacks[event] = callback;
      }
      once(event: string, callback: (event?: unknown) => void) {
        this.callbacks[event] = callback;
      }
      addControl() {}
      addSource() {}
      addLayer() {}
      fitBounds() {}
      queryRenderedFeatures() {
        return this.visible;
      }
    },
    NavigationControl: class {},
    LngLatBounds: class {
      extend() {}
      isEmpty() {
        return true;
      }
    },
  },
}));
afterEach(() => {
  cleanup();
  maps.instances.length = 0;
});

describe("FeaturePreview completion", () => {
  it.each([
    [[], [], "No features in this sample."],
    [
      [{ geometry: null, properties: { name: "null row" } }],
      [],
      "no drawable geometry",
    ],
    [
      [
        { geometry: null },
        { geometry: { type: "Point", coordinates: [7, 51] } },
      ],
      [{}],
      "Sample rendered on map",
    ],
  ])(
    "completes empty, null and mixed samples",
    (features, visible, expected) => {
      renderScreen(
        <FeaturePreview data={{ type: "FeatureCollection", features }} />,
      );
      const map = maps.instances.at(-1)!;
      map.visible = visible;
      act(() => {
        map.callbacks.load();
        map.callbacks.idle();
      });
      expect(screen.getByRole("status")).toHaveTextContent(expected);
      expect(screen.queryByText("Rendering sample…")).not.toBeInTheDocument();
    },
  );

  it("resets failures on changed data and ignores stale events", () => {
    const data = {
      type: "FeatureCollection",
      features: [{ geometry: null, properties: { name: "retained" } }],
    };
    function Example() {
      const [sample, setSample] = useState(data);
      return (
        <>
          <button onClick={() => setSample({ ...data })}>Change sample</button>
          <FeaturePreview data={sample} />
        </>
      );
    }
    renderScreen(<Example />);
    const first = maps.instances.at(-1)!;
    act(() => first.callbacks.error({ error: new Error("WebGL failed") }));
    expect(screen.getByText("retained")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Map preview failed");
    fireEvent.click(screen.getByRole("button", { name: "Change sample" }));
    const second = maps.instances.at(-1)!;
    expect(first.remove).toHaveBeenCalled();
    act(() => {
      first.callbacks.error({ error: new Error("stale") });
      second.callbacks.load();
      second.callbacks.idle();
    });
    expect(screen.queryByText(/WebGL failed|stale/)).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(
      "no drawable geometry",
    );
  });
});
