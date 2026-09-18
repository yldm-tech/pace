/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { translate } from "@pace/i18n";

/**
 * English fallbacks for every document title the admin console sets.
 *
 * These live here rather than inline at each `meta` export for two reasons. First, `translate` needs a fallback at every call site (a `meta` export can run before the `admin` namespace has loaded, and an unresolved i18next lookup returns the key itself, which would read "admin.page.titles.general" in the browser tab). Second, keeping them in one table means the suffix below is written once instead of once per page.
 */
const PAGE_TITLES = {
  app: "Pace | Simple, extensible, open-source project management tool.",
  home: "Admin – Instance Setup & Sign-In",
  general: "General Settings",
  email: "Email Settings",
  images: "Images Settings",
  ai: "Artificial Intelligence Settings",
  authentication: "Authentication Settings",
  workspace: "Workspace Management",
  workspace_create: "Create Workspace",
  google: "Google Authentication",
  github: "GitHub Authentication",
  gitlab: "GitLab Authentication",
  gitea: "Gitea Authentication",
} as const;

export type TAdminPageTitleKey = keyof typeof PAGE_TITLES;

/** "Pace" is a brand name and stays untranslated in every locale; only the word around it is localised. */
const TITLE_SUFFIX = "Pace Admin";

/** The app shell title and the unauthenticated landing page read as standalone titles today, so they skip the suffix. */
const STANDALONE_TITLES = new Set<TAdminPageTitleKey>(["app", "home"]);

/**
 * Build a document title for a React Router `meta` export.
 *
 * `meta` is a module-level function, so `useTranslation` is not available and this goes through the non-hook `translate` instead. That also means the title is not reactive: switching languages repaints the page but not the tab, and the new title lands on the next navigation.
 */
export const getAdminPageTitle = (page: TAdminPageTitleKey): string => {
  const title = translate(`admin.page.titles.${page}`, PAGE_TITLES[page]);
  if (STANDALONE_TITLES.has(page)) return title;

  const suffix = translate("admin.page.titles.suffix", TITLE_SUFFIX);
  // The separator lives in the locale data so a locale can reorder or repunctuate it.
  return translate("admin.page.titles.template", `${title} - ${suffix}`, { page: title, suffix });
};

/** Meta descriptions that sit alongside the titles above; same fallback rules apply. */
const PAGE_DESCRIPTIONS = {
  app: "Open-source project management tool to manage work items, sprints, and product roadmaps with peace of mind.",
  home: "Configure your Pace instance or sign in to the admin portal.",
} as const;

export const getAdminPageDescription = (page: keyof typeof PAGE_DESCRIPTIONS): string =>
  translate(`admin.page.descriptions.${page}`, PAGE_DESCRIPTIONS[page]);
