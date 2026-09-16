/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useEffect, useMemo, useRef, useState } from "react";
import { cn } from "../../utils";
import type { TIconsListProps } from "../helper";
import { adjustColorForContrast, DEFAULT_COLORS } from "../helper";
import { LUCIDE_ICONS_LIST } from "../lucide-icons";
import { MATERIAL_ICONS_LIST } from "../material-icons";

type IconRootProps = TIconsListProps & {
  iconType?: "material" | "lucide";
};

/**
 * The icon half of the emoji and icon picker, beside EmojiRoot and laid out to match it: the same padded root, the same sticky header carrying the same search input, and a scrolling body of size-8 buttons.
 *
 * The two icon sets are drawn differently. A lucide entry carries the component to render; a material one carries only a name, which the material-symbols-rounded font turns into a glyph. Both report the same thing to onChange — a name and the colour chosen for it.
 */
export function IconRoot(props: IconRootProps) {
  const { defaultColor, onChange, searchDisabled = false, iconType = "lucide" } = props;
  const [query, setQuery] = useState("");
  const [activeColor, setActiveColor] = useState(defaultColor);
  const searchRef = useRef<HTMLInputElement>(null);

  // Focused on mount the way EmojiRoot does it, rather than with autoFocus, which the a11y rules reject.
  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  const icons = useMemo(() => {
    const term = query.trim().toLowerCase();
    const names =
      iconType === "material"
        ? MATERIAL_ICONS_LIST.map((icon) => ({ name: icon.name, element: undefined }))
        : LUCIDE_ICONS_LIST.map((icon) => ({ name: icon.name, element: icon.element }));
    if (!term) return names;
    return names.filter((icon) => icon.name.toLowerCase().includes(term));
  }, [iconType, query]);

  return (
    <div data-slot="icon-picker" className="isolate flex h-full w-full flex-col rounded-md border-none p-2">
      <div className="sticky top-0 z-10 flex flex-col gap-2 bg-surface-1 px-1.5 py-2">
        {!searchDisabled && (
          <input
            data-slot="icon-picker-search"
            type="text"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search"
            ref={searchRef}
            className="block h-full w-full flex-grow-0 rounded-md border-[0.5px] border-subtle bg-transparent p-0 px-3 py-2 text-16 placeholder-(--text-color-placeholder) focus:border-accent-strong focus:outline-none"
          />
        )}
        <div data-slot="icon-picker-colors" className="flex items-center gap-2">
          {DEFAULT_COLORS.map((color) => (
            <button
              key={color}
              type="button"
              aria-label={color}
              aria-pressed={color === activeColor}
              onClick={() => setActiveColor(color)}
              // The ring rather than a border, so choosing a colour does not move the row.
              className={cn("size-4 flex-shrink-0 rounded-full", color === activeColor && "ring-2 ring-accent-strong")}
              style={{ backgroundColor: adjustColorForContrast(color) }}
            />
          ))}
        </div>
      </div>
      <div data-slot="icon-picker-content" className="relative flex-1 overflow-y-auto outline-none">
        <div data-slot="icon-picker-list" className="grid grid-cols-8 gap-0.5 px-1.5 pb-2 select-none">
          {icons.map((icon) => (
            <button
              key={icon.name}
              type="button"
              aria-label={icon.name}
              data-slot="icon-picker-list-icon"
              onClick={() => onChange({ name: icon.name, color: activeColor })}
              className="hover:bg-accent flex size-8 items-center justify-center rounded-md text-16"
            >
              {icon.element ? (
                <icon.element className="size-4" style={{ color: activeColor }} />
              ) : (
                <span className="material-symbols-rounded text-16" style={{ color: activeColor }}>
                  {icon.name}
                </span>
              )}
            </button>
          ))}
        </div>
        {icons.length === 0 && <p className="px-3 py-2 text-11 text-tertiary">No icons match that search.</p>}
      </div>
    </div>
  );
}
