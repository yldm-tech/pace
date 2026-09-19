/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { describe, it, expect } from "vitest";
import { applyTransform } from "@hypermod/utils";
import * as transformer from "../ui-loader-to-propel-skeleton";

const apply = (source: string) =>
  applyTransform(transformer, source, { parser: "tsx" });

describe("ui-loader-to-propel-skeleton", () => {
  it("moves a sole-specifier import and renames the element and its .Item", async () => {
    const result = await apply(`
      import { Loader } from "@pace/ui";

      export const Placeholder = () => (
        <Loader className="space-y-4">
          <Loader.Item height="36px" width="100%" />
        </Loader>
      );
    `);

    expect(result).toContain(
      `import { Skeleton } from "@pace/propel/skeleton";`
    );
    expect(result).not.toContain("@pace/ui");
    expect(result).toContain(`<Skeleton className="space-y-4">`);
    expect(result).toContain(`<Skeleton.Item height="36px" width="100%" />`);
    expect(result).toContain("</Skeleton>");
  });

  it("splits Loader out and leaves the other @pace/ui specifiers in place", async () => {
    const result = await apply(`
      import { Loader, CustomMenu, Row } from "@pace/ui";

      export const Placeholder = () => <Loader><Loader.Item /></Loader>;
    `);

    expect(result).toContain(
      `import { Skeleton } from "@pace/propel/skeleton";`
    );
    expect(result).toContain(`import { CustomMenu, Row } from "@pace/ui";`);
  });

  it("removes a trailing Loader specifier without leaving a dangling comma", async () => {
    const result = await apply(`
      import { CustomMenu, Loader } from "@pace/ui";

      export const Placeholder = () => <Loader />;
    `);

    expect(result).toContain(`import { CustomMenu } from "@pace/ui";`);
    expect(result).not.toMatch(/,\s*\}/);
  });

  it("keeps the leading comment above both statements", async () => {
    const result = await apply(`
      // ui
      import { Loader, Row } from "@pace/ui";

      export const Placeholder = () => <Loader />;
    `);

    expect(result).toMatch(
      /\/\/ ui\n\s*import \{ Skeleton \} from "@pace\/propel\/skeleton";\n\s*import \{ Row \} from "@pace\/ui";/
    );
  });

  it("handles a multi-line specifier list", async () => {
    const result = await apply(`
      import {
        CustomMenu,
        Loader,
        Row,
      } from "@pace/ui";

      export const Placeholder = () => <Loader />;
    `);

    expect(result).toContain("CustomMenu,");
    expect(result).toContain("Row,");
    expect(result).not.toMatch(/\bLoader\b/);
  });

  it("leaves a same-named local component alone when nothing comes from @pace/ui", async () => {
    const result = await apply(`
      import { Row } from "@pace/ui";

      function Loader() {
        return <div className="animate-pulse" />;
      }

      export const Placeholder = () => <Loader />;
    `);

    expect(result).not.toContain("@pace/propel/skeleton");
    expect(result).toContain("function Loader()");
    expect(result).toContain("<Loader />");
  });

  it("leaves a file that imports no UI library untouched", async () => {
    const result = await apply(`
      import { Loader } from "@/components/loader";

      export const Placeholder = () => <Loader />;
    `);

    expect(result).not.toContain("@pace/propel/skeleton");
    expect(result).toContain(`import { Loader } from "@/components/loader";`);
  });

  it("does not rename a reference shadowed inside a function", async () => {
    const result = await apply(`
      import { Loader } from "@pace/ui";

      export function render() {
        const Loader = () => <span />;
        return <Loader />;
      }
    `);

    expect(result).toContain(
      `import { Skeleton } from "@pace/propel/skeleton";`
    );
    expect(result).toContain("const Loader = () => <span />;");
    expect(result).toContain("return <Loader />;");
  });

  it("keeps an aliased local name and renames nothing", async () => {
    const result = await apply(`
      import { Loader as UiLoader } from "@pace/ui";

      export const Placeholder = () => <UiLoader><UiLoader.Item /></UiLoader>;
    `);

    expect(result).toContain(
      `import { Skeleton as UiLoader } from "@pace/propel/skeleton";`
    );
    expect(result).toContain("<UiLoader><UiLoader.Item /></UiLoader>");
  });

  it("preserves the type-only modifier", async () => {
    const result = await apply(`
      import type { Loader } from "@pace/ui";

      export type Props = { loader: typeof Loader };
    `);

    expect(result).toContain(
      `import type { Skeleton } from "@pace/propel/skeleton";`
    );
    expect(result).toContain("typeof Skeleton");
  });

  it("reprints nothing but the lines it edits, JSX comments included", async () => {
    // This is the shape recast mangled on an earlier sweep: an oxlint directive inside the JSX, a
    // self-closing element next to a text node, and blank lines between siblings.
    const source = `import { Loader } from "@pace/ui";

export const Placeholder = () => (
  <Loader className="grid">
    {/* oxlint-disable-next-line jsx-a11y/no-static-element-interactions */}
    <div onClick={noop}>
      <span /> label
    </div>

    <Loader.Item height="36px" />
  </Loader>
);
`;

    // applyTransform trims what it returns, hence the trim on the expectation rather than on the input.
    expect(await apply(source)).toBe(
      source
        .replaceAll("Loader", "Skeleton")
        .replace(`from "@pace/ui"`, `from "@pace/propel/skeleton"`)
        .trim()
    );
  });
});
