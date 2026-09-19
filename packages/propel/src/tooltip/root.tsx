/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import * as React from "react";
import { Tooltip as BaseTooltip } from "@base-ui/react/tooltip";
import { cn } from "../utils";
import type { TPlacement, TSide, TAlign } from "../utils/placement";
import { convertPlacementToSideAndAlign } from "../utils/placement";

type ITooltipProps = {
  tooltipHeading?: string;
  tooltipContent?: string | React.ReactNode | null;
  position?: TPlacement;
  // React 19 defaults ReactElement's props to `unknown`; Base UI's `render` prop
  // needs a named props shape to accept the element.
  children: React.ReactElement<Record<string, unknown>>;
  disabled?: boolean;
  className?: string;
  openDelay?: number;
  closeDelay?: number;
  isMobile?: boolean;
  side?: TSide;
  align?: TAlign;
  sideOffset?: number;
};

export function Tooltip(props: ITooltipProps) {
  const {
    tooltipHeading,
    tooltipContent,
    position,
    children,
    disabled = false,
    className = "",
    openDelay = 200,
    // `position` wins when it is given, so it must not be defaulted: with `position = "top"` in this list the branch below always took it and `side`/`align` were declared props that could never reach the positioner. The defaults live here instead, and "top"/"center" is exactly what `convertPlacementToSideAndAlign("top")` returned, so every existing call site that passes neither is placed where it was.
    side = "top",
    align = "center",
    sideOffset = 10,
    closeDelay,
    isMobile = false,
  } = props;
  const { finalSide, finalAlign } = React.useMemo(() => {
    if (position) {
      const converted = convertPlacementToSideAndAlign(position);
      return { finalSide: converted.side, finalAlign: converted.align };
    }
    return { finalSide: side, finalAlign: align };
  }, [position, side, align]);

  // No `BaseTooltip.Provider` here on purpose: it exists to share one delay group across many tooltips, so one provider per tooltip is a `FloatingDelayGroup` of one that can never hand off to a neighbour. Mount it once per app root to get the adjacent-instant-open behaviour; the open delay below is passed per tooltip and wins over a provider either way.
  return (
    // Base UI 1.x owns the hover delays on the trigger, not the root, so that one root can serve several detached triggers with different delays.
    <BaseTooltip.Root disabled={disabled}>
      <BaseTooltip.Trigger render={children} delay={openDelay} closeDelay={closeDelay} />
      <BaseTooltip.Portal>
        <BaseTooltip.Positioner
          className={cn(
            "z-50 max-w-xs gap-1 overflow-hidden rounded-lg border border-subtle-1 bg-layer-2 px-2 py-1.5 break-words shadow-overlay-200",
            {
              hidden: isMobile,
            },
            className
          )}
          side={finalSide}
          sideOffset={sideOffset}
          align={finalAlign}
          render={
            <BaseTooltip.Popup>
              {tooltipHeading && <p className="text-caption-md-medium text-primary">{tooltipHeading}</p>}
              {tooltipContent && (
                <p
                  className={cn("text-caption-sm-regular text-secondary", {
                    "mt-1": tooltipHeading && tooltipHeading !== "",
                  })}
                >
                  {tooltipContent}
                </p>
              )}
            </BaseTooltip.Popup>
          }
        />
      </BaseTooltip.Portal>
    </BaseTooltip.Root>
  );
}
