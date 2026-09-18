/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { Fragment } from "react";
import { observer } from "mobx-react";
import Link from "@/lib/navigation/link";
import { usePathname } from "@/lib/navigation";
// icons
import { Menu } from "lucide-react";
import { SettingsOutline } from "@makeplane/propel/icons";
// pace internal packages
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbSeparator,
} from "@makeplane/propel/components/breadcrumb";
import { useTranslation } from "@pace/i18n";
// hooks
import { useTheme } from "@/hooks/store";
// local imports
import { CORE_HEADER_SEGMENT_BRAND_LABELS, CORE_HEADER_SEGMENT_LABEL_KEYS } from "./core";
import { EXTENDED_HEADER_SEGMENT_LABEL_KEYS } from "./extended";

export const HamburgerToggle = observer(function HamburgerToggle() {
  const { t } = useTranslation();
  const { isSidebarCollapsed, toggleSidebar } = useTheme();
  return (
    <button
      type="button"
      aria-label={t("admin.nav.toggle_sidebar")}
      className="group flex size-7 cursor-pointer items-center justify-center rounded-sm bg-layer-1 transition-all hover:bg-layer-1-hover md:hidden"
      onClick={() => toggleSidebar(!isSidebarCollapsed)}
    >
      <Menu size={14} className="text-secondary transition-all group-hover:text-primary" />
    </button>
  );
});

const HEADER_SEGMENT_LABEL_KEYS = {
  ...CORE_HEADER_SEGMENT_LABEL_KEYS,
  ...EXTENDED_HEADER_SEGMENT_LABEL_KEYS,
};

// Function to dynamically generate breadcrumb items based on pathname. It returns the translation key of each segment rather than its label, because translating requires the `useTranslation` hook and this helper runs outside a component.
const generateBreadcrumbItems = (pathname: string) => {
  const pathSegments = pathname.split("/").slice(1); // removing the first empty string.
  pathSegments.pop();

  let currentUrl = "";
  const breadcrumbItems = pathSegments.map((segment) => {
    currentUrl += "/" + segment;
    return {
      segment,
      labelKey: HEADER_SEGMENT_LABEL_KEYS[segment],
      href: currentUrl,
    };
  });
  return breadcrumbItems;
};

export const AdminHeader = observer(function AdminHeader() {
  const { t } = useTranslation();
  const pathName = usePathname();

  const breadcrumbItems = generateBreadcrumbItems(pathName || "").map((item) => ({
    title: item.labelKey
      ? t(item.labelKey)
      : (CORE_HEADER_SEGMENT_BRAND_LABELS[item.segment] ?? item.segment.toUpperCase()),
    href: item.href,
  }));

  return (
    <div className="relative z-10 flex h-header w-full flex-shrink-0 flex-row items-center justify-between gap-x-2 gap-y-4 border-b border-subtle bg-surface-1 p-4">
      <div className="flex w-full flex-grow items-center gap-2 overflow-ellipsis whitespace-nowrap">
        <HamburgerToggle />
        <div>
          <Breadcrumb aria-label={t("admin.page.breadcrumb.aria_label")}>
            <BreadcrumbList>
              <BreadcrumbItem>
                <BreadcrumbLink
                  label={t("admin.page.breadcrumb.settings")}
                  icon={<SettingsOutline className="h-4 w-4 text-tertiary" />}
                  render={<Link href="/general/" />}
                />
              </BreadcrumbItem>
              {breadcrumbItems.map(
                (item) =>
                  item.title && (
                    <Fragment key={item.title}>
                      <BreadcrumbSeparator />
                      <BreadcrumbItem>
                        <BreadcrumbLink label={item.title} render={<Link href={item.href} />} />
                      </BreadcrumbItem>
                    </Fragment>
                  )
              )}
            </BreadcrumbList>
          </Breadcrumb>
        </div>
      </div>
    </div>
  );
});
