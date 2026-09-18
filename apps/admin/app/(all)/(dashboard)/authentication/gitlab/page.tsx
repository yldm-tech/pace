/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useState } from "react";
import { observer } from "mobx-react";
import useSWR from "swr";
import { Switch } from "@makeplane/propel/components/switch";
// pace internal packages
import { useTranslation } from "@pace/i18n";
// assets
import GitlabLogo from "@/app/assets/logos/gitlab-logo.svg?url";
// components
import { AuthenticationMethodCard } from "@/components/authentication/authentication-method-card";
import { PageWrapper } from "@/components/common/page-wrapper";
import { Skeleton } from "@/components/common/skeleton";
import { setPromiseToast } from "@/providers/toast";
// hooks
import { useInstance } from "@/hooks/store";
// helpers
import { getAdminPageTitle } from "@/helpers/page-title";
// types
import type { Route } from "./+types/page";
// local
import { InstanceGitlabConfigForm } from "./form";

const InstanceGitlabAuthenticationPage = observer(function InstanceGitlabAuthenticationPage(
  _props: Route.ComponentProps
) {
  // i18n
  const { t } = useTranslation();
  // store
  const { fetchInstanceConfigurations, formattedConfig, updateInstanceConfigurations } = useInstance();
  // state
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  // config
  const enableGitlabConfig = formattedConfig?.IS_GITLAB_ENABLED ?? "";

  useSWR("INSTANCE_CONFIGURATIONS", () => fetchInstanceConfigurations());

  const updateConfig = async (key: "IS_GITLAB_ENABLED", value: string) => {
    setIsSubmitting(true);

    const payload = {
      [key]: value,
    };

    const updateConfigPromise = updateInstanceConfigurations(payload);

    setPromiseToast(updateConfigPromise, {
      loading: t("admin.auth.saving"),
      success: {
        title: t("admin.auth.saved"),
        message: () =>
          t(value === "1" ? "admin.oauth.toast.enabled" : "admin.oauth.toast.disabled", { provider: "GitLab" }),
      },
      error: {
        title: t("admin.toast.error"),
        message: () => t("admin.auth.save_failed"),
      },
    });

    await updateConfigPromise
      .then(() => {
        setIsSubmitting(false);
      })
      .catch((err) => {
        console.error(err);
        setIsSubmitting(false);
      });
  };
  return (
    <PageWrapper
      customHeader={
        <AuthenticationMethodCard
          name="GitLab"
          description={t("admin.oauth.description", { provider: "GitLab" })}
          icon={<img src={GitlabLogo} height={24} width={24} alt="GitLab Logo" />}
          config={
            <Switch
              checked={Boolean(parseInt(enableGitlabConfig))}
              onCheckedChange={() => {
                if (Boolean(parseInt(enableGitlabConfig)) === true) {
                  updateConfig("IS_GITLAB_ENABLED", "0");
                } else {
                  updateConfig("IS_GITLAB_ENABLED", "1");
                }
              }}
              size="sm"
              disabled={isSubmitting || !formattedConfig}
            />
          }
          disabled={isSubmitting || !formattedConfig}
          withBorder={false}
        />
      }
    >
      {formattedConfig ? (
        <InstanceGitlabConfigForm config={formattedConfig} />
      ) : (
        <Skeleton className="space-y-8">
          <Skeleton.Item height="50px" width="25%" />
          <Skeleton.Item height="50px" />
          <Skeleton.Item height="50px" />
          <Skeleton.Item height="50px" />
          <Skeleton.Item height="50px" width="50%" />
        </Skeleton>
      )}
    </PageWrapper>
  );
});

export const meta: Route.MetaFunction = () => [{ title: getAdminPageTitle("gitlab") }];

export default InstanceGitlabAuthenticationPage;
