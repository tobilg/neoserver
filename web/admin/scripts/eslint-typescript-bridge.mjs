import { registerHooks } from "node:module";

// typescript-eslint 8 intentionally uses the TypeScript 6 compiler API while
// the application is compiled by TypeScript 7. Keep both versions installed
// and redirect only tooling-internal TypeScript loads to the supported API.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (
      specifier === "typescript" &&
      context.parentURL?.includes("node_modules")
    ) {
      return nextResolve("typescript-eslint-ts", context);
    }
    return nextResolve(specifier, context);
  },
});
