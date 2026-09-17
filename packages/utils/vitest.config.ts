import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Everything under test here is a pure function; nothing reaches for a DOM.
    environment: "node",
  },
});
