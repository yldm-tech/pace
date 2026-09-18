/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
import useSWR from "swr";
// components
import { EUserPermissions, EUserPermissionsLevel } from "@pace/constants";
import { useTranslation } from "@pace/i18n";
import { NotAuthorizedView } from "@/components/auth-screens/not-authorized-view";
import { PageHead } from "@/components/shell/page-title";
import { SingleIntegrationCard } from "@/components/integration/single-integration-card";
import { IntegrationsSettingsLoader } from "@/components/skeletons/loader/settings/integration";
// constants
import { APP_INTEGRATIONS } from "@pace/constants";
// hooks
import { useWorkspace } from "@/hooks/store/use-workspace";
import { useUserPermissions } from "@/hooks/store/user";
// services
import { IntegrationService } from "@pace/services";

const integrationService = new IntegrationService();

function WorkspaceIntegrationsPage() {
  // translation
  const { t } = useTranslation();
  // store hooks
  const { currentWorkspace } = useWorkspace();
  const { allowPermissions } = useUserPermissions();

  // derived values
  const isAdmin = allowPermissions([EUserPermissions.ADMIN], EUserPermissionsLevel.WORKSPACE);
  const pageTitle = currentWorkspace?.name ? `${currentWorkspace.name} - Integrations` : undefined;
  const { data: appIntegrations } = useSWR(isAdmin ? APP_INTEGRATIONS : null, () =>
    isAdmin ? integrationService.getAppIntegrationsList() : null
  );

  if (!isAdmin) return <NotAuthorizedView section="settings" className="h-auto" />;

  return (
    <>
      <PageHead title={pageTitle} />
      <section className="w-full overflow-y-auto">
        <div className="flex items-start gap-3 border-b border-subtle py-3.5">
          <h3 className="text-18 font-medium">{t("integrations.integrations")}</h3>
        </div>
        <div>
          {appIntegrations ? (
            appIntegrations.map((integration) => (
              <SingleIntegrationCard key={integration.id} integration={integration} />
            ))
          ) : (
            <IntegrationsSettingsLoader />
          )}
        </div>
      </section>
    </>
  );
}

export default observer(WorkspaceIntegrationsPage);
