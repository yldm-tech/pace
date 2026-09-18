/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
import { useRouter } from "@/lib/navigation";
import useSWR from "swr";
// ui
import { Banner } from "@makeplane/propel/components/banner";
import { ArchiveOutline } from "@pace/propel/icons";
import { useTranslation } from "@pace/i18n";
import { Button } from "@pace/propel/button";
import { Skeleton } from "@pace/propel/skeleton";
// components
import { PageHead } from "@/components/core/page-title";
import { IssueDetailRoot } from "@/components/issues/issue-detail";
// constants
// hooks
import { useIssueDetail } from "@/hooks/store/use-issue-detail";
import { useProject } from "@/hooks/store/use-project";
import type { Route } from "./+types/page";

function ArchivedIssueDetailsPage({ params }: Route.ComponentProps) {
  // router
  const { workspaceSlug, projectId, archivedIssueId } = params;
  const router = useRouter();
  // states
  // hooks
  const { t } = useTranslation();
  const {
    fetchIssue,
    issue: { getIssueById },
  } = useIssueDetail();

  const { getProjectById } = useProject();

  const { isLoading } = useSWR(`ARCHIVED_ISSUE_DETAIL_${workspaceSlug}_${projectId}_${archivedIssueId}`, () =>
    fetchIssue(workspaceSlug, projectId, archivedIssueId)
  );

  // derived values
  const issue = getIssueById(archivedIssueId);
  const project = issue ? getProjectById(issue?.project_id ?? "") : undefined;
  const pageTitle = project && issue ? `${project?.identifier}-${issue?.sequence_id} ${issue?.name}` : undefined;

  if (!issue) return <></>;

  const issueLoader = !issue || isLoading;

  return (
    <>
      <PageHead title={pageTitle} />
      {issueLoader ? (
        <Skeleton className="flex h-full gap-5 p-5">
          <div className="basis-2/3 space-y-2">
            <Skeleton.Item height="30px" width="40%" />
            <Skeleton.Item height="15px" width="60%" />
            <Skeleton.Item height="15px" width="60%" />
            <Skeleton.Item height="15px" width="40%" />
          </div>
          <div className="basis-1/3 space-y-3">
            <Skeleton.Item height="30px" />
            <Skeleton.Item height="30px" />
            <Skeleton.Item height="30px" />
            <Skeleton.Item height="30px" />
          </div>
        </Skeleton>
      ) : (
        <>
          <Banner
            placement="page"
            variant="warning"
            title={t("issue.archive.banner_message")}
            icon={<ArchiveOutline />}
            actions={
              <Button
                variant="secondary"
                onClick={() => router.push(`/${workspaceSlug}/projects/${projectId}/archives/issues/`)}
              >
                {t("issue.archive.go_to_archives")}
              </Button>
            }
          />
          <div className="flex h-full overflow-hidden">
            <div className="h-full w-full space-y-3 divide-y-2 divide-subtle-1 overflow-y-auto">
              <IssueDetailRoot
                workspaceSlug={workspaceSlug}
                projectId={projectId}
                issueId={archivedIssueId}
                is_archived
              />
            </div>
          </div>
        </>
      )}
    </>
  );
}

export default observer(ArchivedIssueDetailsPage);
