/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
import Link from "@/lib/navigation/link";
import { useTheme } from "next-themes";
// pace imports
import { MARKETING_SITE_URL } from "@pace/constants";
import { Button, getButtonStyling } from "@pace/propel/button";
import { cn } from "@pace/utils";
// assets
import ProjectDarkEmptyState from "@/app/assets/empty-state/project-settings/no-projects-dark.png?url";
import ProjectLightEmptyState from "@/app/assets/empty-state/project-settings/no-projects-light.png?url";
// hooks
import { useCommandPalette } from "@/hooks/store/use-command-palette";

function ProjectSettingsPage() {
  // store hooks
  const { resolvedTheme } = useTheme();
  const { toggleCreateProjectModal } = useCommandPalette();
  // derived values
  const resolvedPath = resolvedTheme === "dark" ? ProjectDarkEmptyState : ProjectLightEmptyState;
  return (
    <div className="mx-auto flex h-full max-w-[480px] flex-col items-center justify-center gap-4">
      <img src={resolvedPath} alt="No projects yet" />
      <div className="text-16 font-semibold text-tertiary">No projects yet</div>
      <div className="text-center text-13 text-tertiary">
        Projects act as the foundation for goal-driven work. They let you manage your teams, tasks, and everything you
        need to get things done.
      </div>
      <div className="flex gap-2">
        {/* This was the marketing site's front page upstream, and the domain rename turned it into a link from the app back to the app's own root. It is dropped when there is no marketing site configured; the primary action next to it is the one that actually does something here. */}
        {MARKETING_SITE_URL && (
          <Link href={MARKETING_SITE_URL} target="_blank" className={cn(getButtonStyling("secondary", "base"))}>
            Learn more about projects
          </Link>
        )}
        <Button onClick={() => toggleCreateProjectModal(true)}>Start your first project</Button>
      </div>
    </div>
  );
}

export default observer(ProjectSettingsPage);
