/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import * as React from "react";

// Pace logo lockup. Not part of @makeplane/propel's icon set, so it is kept as a local asset. The drawing is packages/brand/lockup.svg.
type PaceLockupProps = {
  width?: string | number;
  height?: string | number;
  className?: string;
  color?: string;
};

export function PaceLockup({ width = "242", height = "64", className, color = "currentColor" }: PaceLockupProps) {
  return (
    <svg
      width={width}
      height={height}
      viewBox="0 0 242 64"
      fill={color}
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      <g transform="skewX(-8) translate(6 0)">
        <path
          fillRule="evenodd"
          transform="translate(2.00 0)"
          d="M8 0H24A20 20 0 0 1 24 40V64H8Z M24 12A8 8 0 0 1 24 28Z"
        />
        <path
          d="M67.00 57.00L84.00 7.00"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M84.00 7.00L101.00 57.00"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M72.48 44.80L95.52 44.80"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M169.39 12.30A25.00 25.00 0 1 0 169.39 51.70"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
        />
        <path
          d="M207.00 7.00L207.00 57.00"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M207.00 7.00L235.00 7.00"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M207.00 32.00L228.00 32.00"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <path
          d="M207.00 57.00L235.00 57.00"
          fill="none"
          stroke={color}
          strokeWidth="14.0"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </g>
    </svg>
  );
}
