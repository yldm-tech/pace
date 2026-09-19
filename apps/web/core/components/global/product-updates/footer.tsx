/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { Fragment } from "react";
import { CHANGELOG_URL, DOCS_URL, FORUM_URL, MARKETING_PAGES_PAGE_LINK, SUPPORT_EMAIL } from "@pace/constants";
import { useTranslation } from "@pace/i18n";
// ui
import { getButtonStyling } from "@pace/propel/button";
import { PaceLogo } from "@pace/propel/icons";
// helpers
import { cn } from "@pace/utils";

const LINK_CLASSNAME = "text-13 text-secondary underline-offset-1 outline-none hover:text-primary hover:underline";

export function ProductUpdatesFooter() {
  const { t } = useTranslation();
  // Every destination in this row is a separate site that a self-hosted installation may not have, so the row is built from whichever of them is configured instead of being written out link by link. The dot separator is drawn before each link except the first, because separators written between fixed links leave a dangling dot the moment one of those links stops rendering.
  const links = [
    { key: "docs", label: t("docs"), href: DOCS_URL },
    { key: "changelog", label: t("full_changelog"), href: CHANGELOG_URL },
    { key: "support", label: t("support"), href: SUPPORT_EMAIL ? `mailto:${SUPPORT_EMAIL}` : "" },
    { key: "forum", label: "Forum", href: FORUM_URL },
  ].filter((link) => !!link.href);

  // Nothing here is part of the changelog itself, so with no configured link left there is no footer to draw -- only its margins.
  if (links.length === 0 && !MARKETING_PAGES_PAGE_LINK) return null;

  return (
    <div className="m-6 mb-4 flex flex-shrink-0 items-center justify-between gap-4">
      <div className="flex items-center gap-2">
        {links.map((link, index) => (
          <Fragment key={link.key}>
            {index > 0 && (
              <svg viewBox="0 0 2 2" className="h-0.5 w-0.5 fill-current">
                <circle cx={1} cy={1} r={1} />
              </svg>
            )}
            <a href={link.href} target="_blank" className={LINK_CLASSNAME} rel="noreferrer">
              {link.label}
            </a>
          </Fragment>
        ))}
      </div>
      {MARKETING_PAGES_PAGE_LINK && (
        <a
          href={MARKETING_PAGES_PAGE_LINK}
          target="_blank"
          className={cn(
            getButtonStyling("secondary", "base"),
            "flex items-center gap-1.5 text-center font-medium underline-offset-2 outline-none hover:underline"
          )}
          rel="noreferrer"
        >
          <PaceLogo className="h-4 w-auto text-primary" />
          {t("powered_by_plane_pages")}
        </a>
      )}
    </div>
  );
}
