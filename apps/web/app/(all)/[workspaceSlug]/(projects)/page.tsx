/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
// components
import { useTranslation } from "@pace/i18n";
import { AppHeader } from "@/components/shell/app-header";
import { ContentWrapper } from "@/components/shell/content-wrapper";
import { PageHead } from "@/components/shell/page-title";
import { WorkspaceHomeView } from "@/components/home";
// hooks
import { useWorkspace } from "@/hooks/store/use-workspace";
// local components
import { WorkspaceDashboardHeader } from "./header";

function WorkspaceDashboardPage() {
  const { currentWorkspace } = useWorkspace();
  const { t } = useTranslation();
  // derived values
  const pageTitle = currentWorkspace?.name ? `${currentWorkspace?.name} - ${t("home.title")}` : undefined;

  return (
    <>
      <AppHeader header={<WorkspaceDashboardHeader />} />
      <ContentWrapper>
        <PageHead title={pageTitle} />
        <WorkspaceHomeView />
      </ContentWrapper>
    </>
  );
}

export default observer(WorkspaceDashboardPage);
