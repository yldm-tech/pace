/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// pace imports
import { Tab, TabsList } from "@makeplane/propel/components/tabs";
import { useTranslation } from "@pace/i18n";
// pace web components
import { ORDERED_PAGE_NAVIGATION_TABS_LIST } from "@/components/pages/navigation-pane/tab-panels";

export function PageNavigationPaneTabsList() {
  // translation
  const { t } = useTranslation();

  return (
    <div className="mx-3.5">
      <TabsList>
        {ORDERED_PAGE_NAVIGATION_TABS_LIST.map((tab) => (
          <Tab key={tab.key} value={tab.key} label={t(tab.i18n_label)} />
        ))}
      </TabsList>
    </div>
  );
}
