/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import React, { useState } from "react";
import { observer } from "mobx-react";
import { HelpOutline, PagesOutline, UserOutline } from "@makeplane/propel/icons";
import { DOCS_URL, FORUM_URL, SUPPORT_EMAIL } from "@pace/constants";
import { useTranslation } from "@pace/i18n";
// ui
import { CustomMenu } from "@pace/ui";
// components
import { ProductUpdatesModal } from "@/components/global";
import { AppSidebarItem } from "@/components/sidebar/sidebar-item";
import { PlaneVersionNumber } from "@/components/global/version-number";
// hooks
import { usePowerK } from "@/hooks/store/use-power-k";

export const HelpMenuRoot = observer(function HelpMenuRoot() {
  // store hooks
  const { t } = useTranslation();
  const { toggleShortcutsListModal } = usePowerK();
  // states
  const [isNeedHelpOpen, setIsNeedHelpOpen] = useState(false);
  const [isProductUpdatesModalOpen, setProductUpdatesModalOpen] = useState(false);

  return (
    <>
      <ProductUpdatesModal isOpen={isProductUpdatesModalOpen} handleClose={() => setProductUpdatesModalOpen(false)} />

      <CustomMenu
        customButton={
          <AppSidebarItem
            variant="button"
            item={{
              icon: <HelpOutline className="size-5" />,
              isActive: isNeedHelpOpen,
            }}
          />
        }
        // customButtonClassName="relative grid place-items-center rounded-md p-1.5 outline-none"
        menuButtonOnClick={() => !isNeedHelpOpen && setIsNeedHelpOpen(true)}
        onMenuClose={() => setIsNeedHelpOpen(false)}
        placement="bottom-end"
        maxHeight="lg"
        closeOnSelect
      >
        {/* The documentation site, the support address and the forum are all per-installation configuration this fork ships empty, and each entry is dropped when its own is missing: a menu row whose only job is to open a link has nothing to do without one. The divider goes with them, so the menu does not open on a rule with nothing above it. */}
        {DOCS_URL && (
          <CustomMenu.MenuItem onClick={() => window.open(DOCS_URL, "_blank")}>
            <div className="flex items-center gap-x-2 rounded-sm text-11">
              <PagesOutline className="h-3.5 w-3.5 text-secondary" height={14} width={14} />
              <span className="text-11">{t("documentation")}</span>
            </div>
          </CustomMenu.MenuItem>
        )}
        {SUPPORT_EMAIL && (
          <CustomMenu.MenuItem onClick={() => window.open(`mailto:${SUPPORT_EMAIL}`, "_blank")}>
            <div className="flex items-center gap-x-2 rounded-sm text-11">
              <UserOutline className="h-3.5 w-3.5 text-secondary" width={14} height={14} />
              <span className="text-11">{t("contact_sales")}</span>
            </div>
          </CustomMenu.MenuItem>
        )}
        {(DOCS_URL || SUPPORT_EMAIL) && <div className="my-1 border-t border-subtle" />}
        <CustomMenu.MenuItem>
          <button
            type="button"
            onClick={() => toggleShortcutsListModal(true)}
            className="justify-sbg-layer-211 flex w-full items-center hover:bg-layer-1"
          >
            <span className="text-11">{t("keyboard_shortcuts")}</span>
          </button>
        </CustomMenu.MenuItem>
        <CustomMenu.MenuItem>
          <button
            type="button"
            onClick={() => setProductUpdatesModalOpen(true)}
            className="justify-sbg-layer-211 flex w-full items-center hover:bg-layer-1"
          >
            <span className="text-11">{t("whats_new")}</span>
          </button>
        </CustomMenu.MenuItem>
        {FORUM_URL && (
          <CustomMenu.MenuItem onClick={() => window.open(FORUM_URL, "_blank", "noopener,noreferrer")}>
            <div className="flex items-center gap-x-2 rounded-sm text-11">
              <span className="text-11">Forum</span>
            </div>
          </CustomMenu.MenuItem>
        )}
        <div className="mt-1 border-t border-subtle px-1 pt-2 text-11 text-secondary">
          <PlaneVersionNumber />
        </div>
      </CustomMenu>
    </>
  );
});
