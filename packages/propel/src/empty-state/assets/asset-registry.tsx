/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import React from "react";
import type {
  CompactAssetType,
  DetailedAssetType,
  HorizontalStackAssetType,
  IllustrationAssetType,
  VerticalStackAssetType,
} from "./asset-types";

export type TIllustrationAsset = {
  Component: React.LazyExoticComponent<React.ComponentType<{ className?: string }>>;
  /** Intrinsic size of the illustration's root svg, mirrored here so the Suspense placeholder reserves the exact same box. */
  width: number;
  height: number;
};

/** Every illustration is a few hundred lines of inline SVG, so the registry only holds a loader for each one and the illustration is fetched the first time an empty state asks for it. */
const lazyIllustration = (
  width: number,
  height: number,
  loader: () => Promise<{ default: React.ComponentType<{ className?: string }> }>
): TIllustrationAsset => ({ Component: React.lazy(loader), width, height });

const renderIllustration = ({ Component, width, height }: TIllustrationAsset, className?: string): React.ReactNode => (
  <React.Suspense fallback={<svg width={width} height={height} className={className} />}>
    <Component className={className} />
  </React.Suspense>
);

// Horizontal Stack Asset Registry
export const HORIZONTAL_STACK_ASSETS: Record<HorizontalStackAssetType, TIllustrationAsset> = {
  customer: lazyIllustration(81, 91, () =>
    import("./horizontal-stack/customer").then((m) => ({ default: m.CustomerHorizontalStackIllustration }))
  ),
  epic: lazyIllustration(81, 92, () =>
    import("./horizontal-stack/epic").then((m) => ({ default: m.EpicHorizontalStackIllustration }))
  ),
  estimate: lazyIllustration(81, 92, () =>
    import("./horizontal-stack/estimate").then((m) => ({ default: m.EstimateHorizontalStackIllustration }))
  ),
  export: lazyIllustration(71, 80, () =>
    import("./horizontal-stack/export").then((m) => ({ default: m.ExportHorizontalStackIllustration }))
  ),
  intake: lazyIllustration(71, 80, () =>
    import("./horizontal-stack/intake").then((m) => ({ default: m.IntakeHorizontalStackIllustration }))
  ),
  label: lazyIllustration(81, 92, () =>
    import("./horizontal-stack/label").then((m) => ({ default: m.LabelHorizontalStackIllustration }))
  ),
  link: lazyIllustration(51, 58, () =>
    import("./horizontal-stack/link").then((m) => ({ default: m.LinkHorizontalStackIllustration }))
  ),
  members: lazyIllustration(71, 80, () =>
    import("./horizontal-stack/members").then((m) => ({ default: m.MembersHorizontalStackIllustration }))
  ),
  note: lazyIllustration(51, 58, () =>
    import("./horizontal-stack/note").then((m) => ({ default: m.NoteHorizontalStackIllustration }))
  ),
  priority: lazyIllustration(71, 80, () =>
    import("./horizontal-stack/priority").then((m) => ({ default: m.PriorityHorizontalStackIllustration }))
  ),
  project: lazyIllustration(102, 115, () =>
    import("./horizontal-stack/project").then((m) => ({ default: m.ProjectHorizontalStackIllustration }))
  ),
  settings: lazyIllustration(71, 80, () =>
    import("./horizontal-stack/settings").then((m) => ({ default: m.SettingsHorizontalStackIllustration }))
  ),
  state: lazyIllustration(72, 81, () =>
    import("./horizontal-stack/state").then((m) => ({ default: m.StateHorizontalStackIllustration }))
  ),
  template: lazyIllustration(81, 92, () =>
    import("./horizontal-stack/template").then((m) => ({ default: m.TemplateHorizontalStackIllustration }))
  ),
  token: lazyIllustration(81, 92, () =>
    import("./horizontal-stack/token").then((m) => ({ default: m.TokenHorizontalStackIllustration }))
  ),
  unknown: lazyIllustration(71, 80, () =>
    import("./horizontal-stack/unknown").then((m) => ({ default: m.UnknownHorizontalStackIllustration }))
  ),
  update: lazyIllustration(72, 81, () =>
    import("./horizontal-stack/update").then((m) => ({ default: m.UpdateHorizontalStackIllustration }))
  ),
  webhook: lazyIllustration(81, 91, () =>
    import("./horizontal-stack/webhook").then((m) => ({ default: m.WebhookHorizontalStackIllustration }))
  ),
  "work-item": lazyIllustration(71, 80, () =>
    import("./horizontal-stack/work-item").then((m) => ({ default: m.WorkItemHorizontalStackIllustration }))
  ),
  worklog: lazyIllustration(81, 91, () =>
    import("./horizontal-stack/worklog").then((m) => ({ default: m.WorklogHorizontalStackIllustration }))
  ),
};

// Vertical Stack Asset Registry
export const VERTICAL_STACK_ASSETS: Record<VerticalStackAssetType, TIllustrationAsset> = {
  "archived-cycle": lazyIllustration(161, 174, () =>
    import("./vertical-stack/archived-cycle").then((m) => ({ default: m.ArchivedCycleVerticalStackIllustration }))
  ),
  "archived-module": lazyIllustration(161, 182, () =>
    import("./vertical-stack/archived-module").then((m) => ({ default: m.ArchivedModuleVerticalStackIllustration }))
  ),
  "archived-work-item": lazyIllustration(161, 176, () =>
    import("./vertical-stack/archived-work-item").then((m) => ({
      default: m.ArchivedWorkItemVerticalStackIllustration,
    }))
  ),
  changelog: lazyIllustration(162, 172, () =>
    import("./vertical-stack/changelog").then((m) => ({ default: m.ChangelogVerticalStackIllustration }))
  ),
  customer: lazyIllustration(162, 171, () =>
    import("./vertical-stack/customer").then((m) => ({ default: m.CustomerVerticalStackIllustration }))
  ),
  cycle: lazyIllustration(160, 165, () =>
    import("./vertical-stack/cycle").then((m) => ({ default: m.CycleVerticalStackIllustration }))
  ),
  dashboard: lazyIllustration(162, 165, () =>
    import("./vertical-stack/dashboard").then((m) => ({ default: m.DashboardVerticalStackIllustration }))
  ),
  draft: lazyIllustration(162, 175, () =>
    import("./vertical-stack/draft").then((m) => ({ default: m.DraftVerticalStackIllustration }))
  ),
  epic: lazyIllustration(162, 166, () =>
    import("./vertical-stack/epic").then((m) => ({ default: m.EpicVerticalStackIllustration }))
  ),
  "error-404": lazyIllustration(161, 173, () =>
    import("./vertical-stack/404-error").then((m) => ({ default: m.Error404VerticalStackIllustration }))
  ),
  initiative: lazyIllustration(162, 171, () =>
    import("./vertical-stack/initiative").then((m) => ({ default: m.InitiativeVerticalStackIllustration }))
  ),
  "invalid-link": lazyIllustration(161, 151, () =>
    import("./vertical-stack/invalid-link").then((m) => ({ default: m.InvalidLinkVerticalStackIllustration }))
  ),
  module: lazyIllustration(162, 168, () =>
    import("./vertical-stack/module").then((m) => ({ default: m.ModuleVerticalStackIllustration }))
  ),
  "no-access": lazyIllustration(161, 169, () =>
    import("./vertical-stack/no-access").then((m) => ({ default: m.NoAccessVerticalStackIllustration }))
  ),
  page: lazyIllustration(162, 177, () =>
    import("./vertical-stack/page").then((m) => ({ default: m.PageVerticalStackIllustration }))
  ),
  project: lazyIllustration(160, 143, () =>
    import("./vertical-stack/project").then((m) => ({ default: m.ProjectVerticalStackIllustration }))
  ),
  "server-error": lazyIllustration(162, 184, () =>
    import("./vertical-stack/server-error").then((m) => ({ default: m.ServerErrorVerticalStackIllustration }))
  ),
  teamspace: lazyIllustration(162, 188, () =>
    import("./vertical-stack/teamspace").then((m) => ({ default: m.TeamspaceVerticalStackIllustration }))
  ),
  view: lazyIllustration(162, 163, () =>
    import("./vertical-stack/view").then((m) => ({ default: m.ViewVerticalStackIllustration }))
  ),
  "work-item": lazyIllustration(162, 180, () =>
    import("./vertical-stack/work-item").then((m) => ({ default: m.WorkItemVerticalStackIllustration }))
  ),
};

// Illustration Asset Registry
export const ILLUSTRATION_ASSETS: Record<IllustrationAssetType, TIllustrationAsset> = {
  inbox: lazyIllustration(100, 92, () =>
    import("./illustration/inbox").then((m) => ({ default: m.InboxIllustration }))
  ),
  search: lazyIllustration(161, 168, () =>
    import("./illustration/search").then((m) => ({ default: m.SearchIllustration }))
  ),
};

// Helper functions to get assets
export const getCompactAsset = (assetKey: CompactAssetType, className?: string): React.ReactNode => {
  const asset =
    HORIZONTAL_STACK_ASSETS[assetKey as HorizontalStackAssetType] ||
    ILLUSTRATION_ASSETS[assetKey as IllustrationAssetType];

  if (!asset) {
    console.warn(`Asset "${assetKey}" not found in compact asset registry`);
    return null;
  }

  return renderIllustration(asset, className);
};

export const getDetailedAsset = (assetKey: DetailedAssetType, className?: string): React.ReactNode => {
  const asset =
    VERTICAL_STACK_ASSETS[assetKey as VerticalStackAssetType] || ILLUSTRATION_ASSETS[assetKey as IllustrationAssetType];

  if (!asset) {
    console.warn(`Asset "${assetKey}" not found in detailed asset registry`);
    return null;
  }

  return renderIllustration(asset, className);
};
