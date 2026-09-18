/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// Translation keys for the breadcrumb label of each path segment. The values are i18n keys, not display text: the header component resolves them with `t()` because a module-scope constant cannot call the `useTranslation` hook.
export const CORE_HEADER_SEGMENT_LABEL_KEYS: Record<string, string> = {
  general: "admin.page.breadcrumb.general",
  ai: "admin.page.breadcrumb.ai",
  email: "admin.page.breadcrumb.email",
  authentication: "admin.page.breadcrumb.authentication",
  image: "admin.page.breadcrumb.image",
  workspace: "admin.page.breadcrumb.workspace",
  create: "admin.page.breadcrumb.create",
};

// Segments whose label is a brand name. These render verbatim in every locale, so they stay out of the translation catalog.
export const CORE_HEADER_SEGMENT_BRAND_LABELS: Record<string, string> = {
  google: "Google",
  github: "GitHub",
  gitlab: "GitLab",
  gitea: "Gitea",
};
