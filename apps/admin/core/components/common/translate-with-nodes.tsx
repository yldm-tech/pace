/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import React from "react";
// pace internal packages
import type { TTranslationStore } from "@pace/i18n";

// A slot marker is the slot name fenced by a private-use code point, which can never appear in translated copy.
const SLOT_FENCE = "\uE000";
const SLOT_MARKER_PATTERN = new RegExp(`${SLOT_FENCE}([a-zA-Z_]+)${SLOT_FENCE}`);

const slotMarker = (name: string): string => `${SLOT_FENCE}${name}${SLOT_FENCE}`;

type TNodeSlots = Record<string, React.ReactNode>;

/**
 * Renders one ICU message whose placeholders stand for React nodes, e.g. a code chip or a link.
 *
 * t() can only return a string, so every node slot is formatted as a marker and the formatted message is split back apart around those markers. That keeps each sentence a single translatable key: translators move `{field}` and `{link}` wherever their grammar needs them instead of being handed English-ordered fragments to concatenate.
 */
export function translateWithNodes(
  t: TTranslationStore["t"],
  key: string,
  slots: TNodeSlots,
  values: Record<string, unknown> = {}
): React.ReactNode {
  const markers: Record<string, string> = {};
  for (const name of Object.keys(slots)) {
    markers[name] = slotMarker(name);
  }

  // split() with a capturing group interleaves the captured slot names at the odd indexes.
  const parts = t(key, { ...values, ...markers }).split(SLOT_MARKER_PATTERN);

  return (
    <>
      {parts.map((part, index) =>
        index % 2 === 1 ? <React.Fragment key={part}>{slots[part] ?? null}</React.Fragment> : part
      )}
    </>
  );
}
