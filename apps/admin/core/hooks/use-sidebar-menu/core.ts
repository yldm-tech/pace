/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { BrainCog } from "lucide-react";
// pace imports
import { ImageOutline, LockOutline, MailOutline, SettingsOutline, WorkspaceOutline } from "@makeplane/propel/icons";
// types
import type { TSidebarMenuItem } from "./types";

export type TCoreSidebarMenuKey = "general" | "email" | "workspace" | "authentication" | "ai" | "image";

// `name` and `description` hold translation keys, not text: this is a module-level constant, so it cannot call the translation hook. The keys are resolved where the menu is rendered, in app/(all)/(dashboard)/sidebar-menu.tsx.
export const coreSidebarMenuLinks: Record<TCoreSidebarMenuKey, TSidebarMenuItem> = {
  general: {
    Icon: SettingsOutline,
    name: "admin.nav.general",
    description: "admin.nav.general_description",
    href: `/general/`,
  },
  email: {
    Icon: MailOutline,
    name: "admin.nav.email",
    description: "admin.nav.email_description",
    href: `/email/`,
  },
  workspace: {
    Icon: WorkspaceOutline,
    name: "admin.nav.workspace",
    description: "admin.nav.workspace_description",
    href: `/workspace/`,
  },
  authentication: {
    Icon: LockOutline,
    name: "admin.nav.authentication",
    description: "admin.nav.authentication_description",
    href: `/authentication/`,
  },
  ai: {
    Icon: BrainCog,
    name: "admin.nav.ai",
    description: "admin.nav.ai_description",
    href: `/ai/`,
  },
  image: {
    Icon: ImageOutline,
    name: "admin.nav.images",
    description: "admin.nav.images_description",
    href: `/image/`,
  },
};
