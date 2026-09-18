/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
// components
import { PageWrapper } from "@/components/common/page-wrapper";
// helpers
import { getAdminPageTitle } from "@/helpers/page-title";
// types
import type { Route } from "./+types/page";
// local
import { WorkspaceCreateForm } from "./form";
import { useTranslation } from "@pace/i18n";

const WorkspaceCreatePage = observer(function WorkspaceCreatePage(_props: Route.ComponentProps) {
  const { t } = useTranslation();

  return (
    <PageWrapper
      header={{
        title: t("admin.page.workspace_create.title"),
        description: t("admin.page.workspace_create.description"),
      }}
    >
      <WorkspaceCreateForm />
    </PageWrapper>
  );
});

export const meta: Route.MetaFunction = () => [{ title: getAdminPageTitle("workspace_create") }];

export default WorkspaceCreatePage;
