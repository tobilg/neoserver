import "@testing-library/jest-dom/vitest";

// jsdom does not implement layout observers used by Radix form controls.
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
};
