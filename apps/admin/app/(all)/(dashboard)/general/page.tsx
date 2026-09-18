/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
// components
import { PageWrapper } from "@/components/common/page-wrapper";
// hooks
import { useInstance } from "@/hooks/store";
// local imports
import { GeneralConfigurationForm } from "./form";
// helpers
import { getAdminPageTitle } from "@/helpers/page-title";
// types
import type { Route } from "./+types/page";
import { useTranslation } from "@pace/i18n";

function GeneralPage() {
  const { t } = useTranslation();
  const { instance, instanceAdmins } = useInstance();

  return (
    <PageWrapper
      header={{
        title: t("admin.page.general.title"),
        description: t("admin.page.general.description"),
      }}
    >
      {instance && instanceAdmins && <GeneralConfigurationForm instance={instance} instanceAdmins={instanceAdmins} />}
    </PageWrapper>
  );
}

export const meta: Route.MetaFunction = () => [{ title: getAdminPageTitle("general") }];

export default observer(GeneralPage);
