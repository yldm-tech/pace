/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

export const NAMESPACES = [
  "accessibility",
  "admin",
  "auth",
  "automation",
  "common",
  "cycle",
  "editor",
  "empty-state",
  "home",
  "inbox",
  "integration",
  "module",
  "navigation",
  "notification",
  "page",
  "power-k",
  "project",
  "project-settings",
  "settings",
  "stickies",
  "template",
  "tour",
  "update",
  "wiki",
  "work-item",
  "work-item-type",
  "workflow",
  "workspace",
  "workspace-settings",
] as const;

export type TNamespace = (typeof NAMESPACES)[number];

export const DEFAULT_NAMESPACE: TNamespace = "common";

// The namespaces an app downloads before it renders. Every key the admin console looks up lives in `admin`, and no shared package it depends on calls `t()` at all, so loading the other twenty-seven -- work items, cycles, wikis, stickies, screens the console does not have -- only delays its first paint. `common` stays because it is DEFAULT_NAMESPACE and the fallback chain roots there.
//
// `pnpm --filter @pace/i18n check:sync` resolves every `t("...")` in each app against the set it declares here, so a key added outside that set fails CI rather than rendering as its own name in production.
export const ADMIN_NAMESPACES: readonly TNamespace[] = ["admin", "common"];
