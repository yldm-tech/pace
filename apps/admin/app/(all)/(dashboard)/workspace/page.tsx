/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useState } from "react";
import { observer } from "mobx-react";
import Link from "@/lib/navigation/link";
import useSWR from "swr";
import { LoadingOutline as LoaderIcon } from "@makeplane/propel/icons";
// types
import { AnchorButton } from "@makeplane/propel/components/anchor-button";
import { Button } from "@makeplane/propel/components/button";
import { Switch } from "@makeplane/propel/components/switch";
import type { TInstanceConfigurationKeys } from "@pace/types";
import { cn } from "@pace/utils";
// components
import { PageWrapper } from "@/components/common/page-wrapper";
import { Skeleton } from "@/components/common/skeleton";
import { WorkspaceListItem } from "@/components/workspace/list-item";
import { setPromiseToast } from "@/providers/toast";
// hooks
import { useInstance, useWorkspace } from "@/hooks/store";
// helpers
import { getAdminPageTitle } from "@/helpers/page-title";
// types
import type { Route } from "./+types/page";
import { useTranslation } from "@pace/i18n";

const WorkspaceManagementPage = observer(function WorkspaceManagementPage(_props: Route.ComponentProps) {
  const { t } = useTranslation();
  // states
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  // store
  const { formattedConfig, fetchInstanceConfigurations, updateInstanceConfigurations } = useInstance();
  const {
    workspaceIds,
    loader: workspaceLoader,
    paginationInfo,
    fetchWorkspaces,
    fetchNextWorkspaces,
  } = useWorkspace();
  // derived values
  const disableWorkspaceCreation = formattedConfig?.DISABLE_WORKSPACE_CREATION ?? "";
  const hasNextPage = paginationInfo?.next_page_results && paginationInfo?.next_cursor !== undefined;

  // fetch data
  useSWR("INSTANCE_CONFIGURATIONS", () => fetchInstanceConfigurations());
  useSWR("INSTANCE_WORKSPACES", () => fetchWorkspaces());

  const updateConfig = async (key: TInstanceConfigurationKeys, value: string) => {
    setIsSubmitting(true);

    const payload = {
      [key]: value,
    };

    const updateConfigPromise = updateInstanceConfigurations(payload);

    setPromiseToast(updateConfigPromise, {
      loading: t("admin.page.workspace.config_saving"),
      success: {
        title: t("admin.toast.success"),
        message: () => t("admin.page.workspace.config_saved"),
      },
      error: {
        title: t("admin.toast.error"),
        message: () => t("admin.page.workspace.config_save_failed"),
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
      header={{
        title: t("admin.page.workspace.title"),
        description: t("admin.page.workspace.description"),
      }}
    >
      <div className="space-y-3">
        {formattedConfig ? (
          <div className={cn("flex w-full items-center gap-14 rounded-sm")}>
            <div className="flex grow items-center gap-4">
              <div className="grow">
                <div className="pb-1 text-16 font-medium">{t("admin.workspace.prevent_creation")}</div>
                <div className={cn("text-11 leading-5 font-regular text-tertiary")}>
                  {t("admin.page.workspace.prevent_creation_description")}
                </div>
              </div>
            </div>
            <div className={`shrink-0 pr-4 ${isSubmitting && "opacity-70"}`}>
              <div className="flex items-center gap-4">
                <Switch
                  checked={Boolean(parseInt(disableWorkspaceCreation))}
                  onCheckedChange={() => {
                    if (Boolean(parseInt(disableWorkspaceCreation)) === true) {
                      updateConfig("DISABLE_WORKSPACE_CREATION", "0");
                    } else {
                      updateConfig("DISABLE_WORKSPACE_CREATION", "1");
                    }
                  }}
                  size="sm"
                  disabled={isSubmitting}
                />
              </div>
            </div>
          </div>
        ) : (
          <Skeleton>
            <Skeleton.Item height="50px" width="100%" />
          </Skeleton>
        )}
        {workspaceLoader !== "init-loader" ? (
          <>
            <div className="flex items-center justify-between gap-2 pt-6">
              <div className="flex flex-col items-start gap-x-2">
                <div className="flex items-center gap-2 text-16 font-medium">
                  {t("admin.page.workspace.list_heading")}{" "}
                  <span className="text-tertiary">• {workspaceIds.length}</span>
                  {workspaceLoader && ["mutation", "pagination"].includes(workspaceLoader) && (
                    <LoaderIcon className="h-4 w-4 animate-spin" />
                  )}
                </div>
                <div className={cn("text-11 leading-5 font-regular text-tertiary")}>
                  {t("admin.page.workspace.list_description")}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="primary"
                  size="sm"
                  stretch="auto"
                  nativeButton={false}
                  render={<Link href="/workspace/create" />}
                  label={t("admin.workspace.create")}
                />
              </div>
            </div>
            <div className="flex flex-col gap-4 py-2">
              {workspaceIds.map((workspaceId) => (
                <WorkspaceListItem key={workspaceId} workspaceId={workspaceId} />
              ))}
            </div>
            {hasNextPage && (
              <div className="flex justify-center">
                <AnchorButton
                  variant="primary"
                  size="md"
                  onClick={() => fetchNextWorkspaces()}
                  loading={workspaceLoader === "pagination"}
                  label={t("admin.actions.load_more")}
                />
              </div>
            )}
          </>
        ) : (
          <Skeleton className="space-y-10 py-8">
            <Skeleton.Item height="24px" width="20%" />
            <Skeleton.Item height="92px" width="100%" />
            <Skeleton.Item height="92px" width="100%" />
            <Skeleton.Item height="92px" width="100%" />
          </Skeleton>
        )}
      </div>
    </PageWrapper>
  );
});

export const meta: Route.MetaFunction = () => [{ title: getAdminPageTitle("workspace") }];

export default WorkspaceManagementPage;
