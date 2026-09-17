/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import * as React from "react";

import type { ISvgIcons } from "../type";

/**
 * The Pace mark. The path is the same one packages/brand/mark.svg holds, which is what every favicon and app icon in the repository is generated from -- change one and run packages/brand/generate.sh so the other follows.
 */
export function PaceLogo({ width = "46", height = "64", className, color = "currentColor" }: ISvgIcons) {
  return (
    <svg
      width={width}
      height={height}
      viewBox="0 0 46 64"
      fill={color}
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <path
        fillRule="evenodd"
        transform="translate(2 0) skewX(-8)"
        d="M8 0H24A20 20 0 0 1 24 40V64H8Z M24 12A8 8 0 0 1 24 28Z"
        fill={color}
      />
    </svg>
  );
}
