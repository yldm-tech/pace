/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import React from "react";
// helpers
import { Button } from "@pace/propel/button";
import { Tooltip } from "@makeplane/propel/components/tooltip";
import { cn } from "@pace/utils";
// types
import { usePlatformOS } from "@/hooks/use-platform-os";
import { BACKGROUND_BUTTON_VARIANTS, BORDER_BUTTON_VARIANTS } from "./constants";
import type { TButtonVariants } from "./types";

export type DropdownButtonProps = {
  children: React.ReactNode;
  className?: string;
  isActive: boolean;
  tooltipContent?: string;
  tooltipHeading: string;
  showTooltip: boolean;
  variant: TButtonVariants;
  renderToolTipByDefault?: boolean;
};

type ButtonProps = {
  children: React.ReactNode;
  className?: string;
  isActive: boolean;
  tooltipContent?: string;
  tooltipHeading: string;
  showTooltip: boolean;
  renderToolTipByDefault?: boolean;
};

export function DropdownButton(props: DropdownButtonProps) {
  const {
    children,
    className,
    isActive,
    tooltipContent,
    renderToolTipByDefault = true,
    tooltipHeading,
    showTooltip,
    variant,
  } = props;
  const ButtonToRender: React.FC<ButtonProps> = BORDER_BUTTON_VARIANTS.includes(variant)
    ? BorderButton
    : BACKGROUND_BUTTON_VARIANTS.includes(variant)
      ? BackgroundButton
      : TransparentButton;

  return (
    <ButtonToRender
      className={className}
      isActive={isActive}
      tooltipContent={tooltipContent}
      tooltipHeading={tooltipHeading}
      showTooltip={showTooltip}
      renderToolTipByDefault={renderToolTipByDefault}
    >
      {children}
    </ButtonToRender>
  );
}

type TTooltipMount = "deferred" | "pointer" | "focus";

/**
 * Implements `renderToolTipByDefault={false}` by keeping the Base UI tooltip out of the tree until the button is hovered or focused. A list or spreadsheet screen renders hundreds of these buttons, and `disabled` does not save the cost of a mounted `Tooltip.Root` + `Tooltip.Trigger` — only not rendering them does.
 *
 * Mounting the tooltip replaces the underlying `<button>` node, so the two entry paths need different care. The pointer path needs none: Base UI arms its open delay from `mousemove` over the trigger, and the pointer that just entered is still moving. The focus path loses focus along with the node it replaced, so focus is put back on the replacement, which is also what tells Base UI to open the tooltip.
 */
function useDeferredTooltip(renderByDefault: boolean) {
  const buttonRef = React.useRef<HTMLButtonElement>(null);
  const [mount, setMount] = React.useState<TTooltipMount>(renderByDefault ? "pointer" : "deferred");

  React.useLayoutEffect(() => {
    if (mount !== "focus") return;
    const button = buttonRef.current;
    if (!button) return;
    // Discarding the focused node leaves the document focused on <body> (or on nothing); any other active element means focus has already moved on and must not be stolen back.
    const activeElement = button.ownerDocument.activeElement;
    if (activeElement === null || activeElement === button.ownerDocument.body) button.focus();
  }, [mount]);

  return {
    buttonRef,
    isTooltipMounted: mount !== "deferred",
    deferredTriggerProps: {
      onPointerEnter: () => setMount((current) => (current === "deferred" ? "pointer" : current)),
      onFocus: () => setMount((current) => (current === "deferred" ? "focus" : current)),
    },
  };
}

function BorderButton(props: ButtonProps) {
  const {
    children,
    className,
    isActive,
    tooltipContent,
    tooltipHeading,
    showTooltip,
    renderToolTipByDefault = true,
  } = props;
  const { isMobile } = usePlatformOS();
  const { buttonRef, isTooltipMounted, deferredTriggerProps } = useDeferredTooltip(renderToolTipByDefault);

  const button = (
    <Button
      ref={buttonRef}
      variant="ghost"
      size="sm"
      className={cn(
        "flex h-full w-full items-center justify-start gap-1.5 border-[0.5px] border-strong",
        {
          "bg-layer-transparent-active": isActive,
        },
        className
      )}
      {...deferredTriggerProps}
    >
      {children}
    </Button>
  );

  if (!isTooltipMounted) return button;

  return (
    <Tooltip
      label={tooltipContent ? `${tooltipHeading}: ${tooltipContent}` : tooltipHeading}
      layout="stacked"
      disabled={!showTooltip || isMobile}
    >
      {button}
    </Tooltip>
  );
}

function BackgroundButton(props: ButtonProps) {
  const { children, className, tooltipContent, tooltipHeading, showTooltip, renderToolTipByDefault = true } = props;
  const { isMobile } = usePlatformOS();
  const { buttonRef, isTooltipMounted, deferredTriggerProps } = useDeferredTooltip(renderToolTipByDefault);

  const button = (
    <Button
      ref={buttonRef}
      variant="ghost"
      size="sm"
      className={cn(
        "flex h-full w-full items-center justify-between gap-1.5 bg-layer-3 hover:bg-layer-1-hover",
        className
      )}
      {...deferredTriggerProps}
    >
      {children}
    </Button>
  );

  if (!isTooltipMounted) return button;

  return (
    <Tooltip
      label={tooltipContent ? `${tooltipHeading}: ${tooltipContent}` : tooltipHeading}
      layout="stacked"
      disabled={!showTooltip || isMobile}
    >
      {button}
    </Tooltip>
  );
}

function TransparentButton(props: ButtonProps) {
  const {
    children,
    className,
    isActive,
    tooltipContent,
    tooltipHeading,
    showTooltip,
    renderToolTipByDefault = true,
  } = props;
  const { isMobile } = usePlatformOS();
  const { buttonRef, isTooltipMounted, deferredTriggerProps } = useDeferredTooltip(renderToolTipByDefault);

  const button = (
    <Button
      ref={buttonRef}
      variant="ghost"
      size="sm"
      className={cn(
        "flex h-full w-full items-center justify-between gap-1.5",
        {
          "bg-layer-transparent-active": isActive,
        },
        className
      )}
      {...deferredTriggerProps}
    >
      {children}
    </Button>
  );

  if (!isTooltipMounted) return button;

  return (
    <Tooltip
      label={tooltipContent ? `${tooltipHeading}: ${tooltipContent}` : tooltipHeading}
      layout="stacked"
      disabled={!showTooltip || isMobile}
    >
      {button}
    </Tooltip>
  );
}
