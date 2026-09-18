/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { describe, it, expect } from "vitest";
import { applyTransform } from "@hypermod/utils";
import * as transformer from "../icons-to-pace-propel";

const apply = (source: string) =>
  applyTransform(transformer, source, { parser: "tsx" });

describe("icons-to-pace-propel", () => {
  it("rewrites the source and leaves every specifier alone", async () => {
    const result = await apply(`
      import { AddOutline, CloseOutline as Close } from "@makeplane/propel/icons";

      export const Toolbar = () => (
        <span>
          <AddOutline />
          <Close />
        </span>
      );
    `);

    expect(result).toContain(
      `import { AddOutline, CloseOutline as Close } from "@pace/propel/icons";`
    );
    expect(result).not.toContain("@makeplane/propel/icons");
  });

  it("keeps a multi-line specifier list formatted exactly as it was", async () => {
    const result = await apply(`import {
  AddOutline,
  CloseOutline,
  TickOutline,
} from "@makeplane/propel/icons";

export const icons = [AddOutline, CloseOutline, TickOutline];
`);

    // The transform splices the specifier into the original text rather than reprinting the
    // AST, so the diff for this file is one line rather than a reflowed import block.
    expect(result).toBe(`import {
  AddOutline,
  CloseOutline,
  TickOutline,
} from "@pace/propel/icons";

export const icons = [AddOutline, CloseOutline, TickOutline];`);
  });

  it("leaves JSX carrying comments and blank lines byte-identical", async () => {
    // This is the shape that made reprinting through recast unusable: it wrapped the element
    // in parentheses, folded the `<span />` onto the text beside it and dropped the blank
    // lines between siblings, in 7 of the repo's 531 files.
    const before = `import { DeleteOutline } from "SOURCE";

export const Row = () => (
  <div>
    {/* oxlint-disable-next-line jsx_a11y/click-events-have-key-events */}
    <div onClick={remove}>
      <DeleteOutline width={14} />

      <span className="live" />
      Live
    </div>

  </div>
);
`;

    const result = await apply(
      before.replace("SOURCE", "@makeplane/propel/icons")
    );

    expect(result).toBe(
      before.replace("SOURCE", "@pace/propel/icons").trimEnd()
    );
  });

  it("leaves a file that already imports @pace/propel/icons with two separate imports", async () => {
    const result = await apply(`
      import { AddOutline } from "@makeplane/propel/icons";
      import { StateGroupIcon } from "@pace/propel/icons";

      export const Row = () => <span><AddOutline /><StateGroupIcon /></span>;
    `);

    // Merging is deliberately not attempted: it would reflow specifier lists across 52 files,
    // and duplicate imports from one source already exist in the codebase.
    expect(result).toContain(
      `import { AddOutline } from "@pace/propel/icons";`
    );
    expect(result).toContain(
      `import { StateGroupIcon } from "@pace/propel/icons";`
    );
    expect(result).not.toContain("@makeplane/propel/icons");
  });

  it("rewrites type-only imports", async () => {
    const result = await apply(`
      import type { ISvgIcons } from "@makeplane/propel/icons";

      export type Props = { icon: ISvgIcons };
    `);

    expect(result).toContain(
      `import type { ISvgIcons } from "@pace/propel/icons";`
    );
  });

  it("rewrites re-export sources so no barrel keeps the old path", async () => {
    const result = await apply(`
      export { AddOutline } from "@makeplane/propel/icons";
      export * from "@makeplane/propel/icons";
    `);

    expect(result).toContain(
      `export { AddOutline } from "@pace/propel/icons";`
    );
    expect(result).toContain(`export * from "@pace/propel/icons";`);
  });

  it("does not touch the other @makeplane/propel subpaths", async () => {
    const source = `
      import { Switch } from "@makeplane/propel/components/switch";
      import { Avatar } from "@makeplane/propel/components/avatar";
      import { useResolvedTheme } from "@makeplane/propel/hooks";

      export const Row = () => <Switch checked={false} />;
    `;
    const result = await apply(source);

    // A file keeping one component import from the external package is the expected end state
    // for 55 form screens, so the rename must be scoped to the `/icons` subpath alone.
    expect(result).toContain(`from "@makeplane/propel/components/switch"`);
    expect(result).toContain(`from "@makeplane/propel/components/avatar"`);
    expect(result).toContain(`from "@makeplane/propel/hooks"`);
  });

  it("returns the source untouched when there is nothing to rewrite", async () => {
    const source = `import { StateGroupIcon } from "@pace/propel/icons";\n`;

    expect(await apply(source)).toBe(source.trim());
  });
});
