/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { createContext, memo, useContext, useMemo } from "react";
import { Popover as BasePopover } from "@base-ui/react/popover";
import type { TPlacement, TSide, TAlign } from "../utils/placement";
import { convertPlacementToSideAndAlign } from "../utils/placement";

/**
 * Base UI 1.x moved the hover-open props off the popover root and onto the trigger, so that one root can
 * serve several detached triggers with their own delays. `Popover` keeps accepting them on the root — that
 * is the shape every call site passes, and this component's props are its public contract — and hands them
 * down to `Popover.Button` through this context. A prop passed directly on the button still wins.
 */
type TPopoverHoverProps = Pick<BasePopover.Trigger.Props, "openOnHover" | "delay" | "closeDelay">;

const PopoverHoverContext = createContext<TPopoverHoverProps>({});

export interface PopoverProps extends React.ComponentProps<typeof BasePopover.Root>, TPopoverHoverProps {}

export interface PopoverContentProps extends React.ComponentProps<typeof BasePopover.Popup> {
  placement?: TPlacement;
  align?: TAlign;
  sideOffset?: BasePopover.Positioner.Props["sideOffset"];
  side?: TSide;
  containerRef?: React.RefObject<HTMLElement | null>;
  positionerClassName?: string;
}

// PopoverContent component
const PopoverContent = memo(function PopoverContent({
  children,
  className,
  placement,
  side = "bottom",
  align = "center",
  sideOffset = 8,
  containerRef,
  positionerClassName,
  ...props
}: PopoverContentProps) {
  // side and align calculations
  const { finalSide, finalAlign } = useMemo(() => {
    if (placement) {
      const converted = convertPlacementToSideAndAlign(placement);
      return { finalSide: converted.side, finalAlign: converted.align };
    }
    return { finalSide: side, finalAlign: align };
  }, [placement, side, align]);

  return (
    <PopoverPortal container={containerRef?.current}>
      <PopoverPositioner side={finalSide} sideOffset={sideOffset} align={finalAlign} className={positionerClassName}>
        <BasePopover.Popup data-slot="popover-content" className={className} {...props}>
          {children}
        </BasePopover.Popup>
      </PopoverPositioner>
    </PopoverPortal>
  );
});

// wrapper components
const PopoverTrigger = memo(function PopoverTrigger(props: React.ComponentProps<typeof BasePopover.Trigger>) {
  const hoverProps = useContext(PopoverHoverContext);
  return <BasePopover.Trigger data-slot="popover-trigger" {...hoverProps} {...props} />;
});

const PopoverPortal = memo(function PopoverPortal(props: React.ComponentProps<typeof BasePopover.Portal>) {
  return <BasePopover.Portal data-slot="popover-portal" {...props} />;
});

const PopoverPositioner = memo(function PopoverPositioner(props: React.ComponentProps<typeof BasePopover.Positioner>) {
  return <BasePopover.Positioner data-slot="popover-positioner" {...props} />;
});

// compound components
const Popover = Object.assign(
  memo(function Popover({ openOnHover, delay, closeDelay, ...props }: PopoverProps) {
    const hoverProps = useMemo(() => ({ openOnHover, delay, closeDelay }), [openOnHover, delay, closeDelay]);
    return (
      <PopoverHoverContext.Provider value={hoverProps}>
        <BasePopover.Root data-slot="popover" {...props} />
      </PopoverHoverContext.Provider>
    );
  }),
  {
    Button: PopoverTrigger,
    Panel: PopoverContent,
  }
);

// display names
PopoverContent.displayName = "PopoverContent";
Popover.displayName = "Popover";
PopoverPortal.displayName = "PopoverPortal";
PopoverTrigger.displayName = "PopoverTrigger";
PopoverPositioner.displayName = "PopoverPositioner";

export { Popover };
