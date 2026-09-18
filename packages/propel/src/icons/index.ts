/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// The 737 `*Outline`/`*Filled` icons ship from @makeplane/propel; re-exporting them here makes `@pace/propel/icons` the single icon import path for every app, so a call site never has to know which of the two sets an icon came from. The two name sets are disjoint — every local export below is suffixed `*Icon` — so no name is ambiguous and nothing is shadowed.
export * from "@makeplane/propel/icons";

export type { ISvgIcons } from "./type";
export type { IconName } from "./registry";
export { ICON_REGISTRY } from "./registry";
export * from "./actions";
export * from "./activity-icon";
export * from "./ai-icon";
export * from "./arrows";
export * from "./at-risk-icon";
export * from "./attachments";
export * from "./bar-icon";
export * from "./blocked-icon";
export * from "./blocker-icon";
export * from "./brand";
export * from "./calendar-after-icon";
export * from "./calendar-before-icon";
export * from "./center-panel-icon";
export * from "./comment-fill-icon";
export * from "./create-icon";
export * from "./cycle";
export * from "./default-icon";
export * from "./dice-icon";
export * from "./display-properties";
export * from "./done-icon";
export * from "./dropdown-icon";
export * from "./favorite-folder-icon";
export * from "./full-screen-panel-icon";
export * from "./github-icon";
export * from "./gitlab-icon";
export * from "./helpers";
export * from "./icon-wrapper";
export * from "./icon";
export * from "./in-progress-icon";
export * from "./info-fill-icon";
export * from "./intake";
export * from "./layer-stack";
export * from "./layers-icon";
export * from "./layouts";
export * from "./lead-icon";
export * from "./misc";
export * from "./module";
export * from "./monospace-icon";
export * from "./multiple-sticky";
export * from "./off-track-icon";
export * from "./on-track-icon";
export * from "./overview-icon";
export * from "./pending-icon";
export * from "./photo-filter-icon";
export * from "./planned-icon";
export * from "./priority-icon";
export * from "./project";
export * from "./properties";
export * from "./related-icon";
export * from "./sans-serif-icon";
export * from "./serif-icon";
export * from "./set-as-default-icon";
export * from "./side-panel-icon";
export * from "./state";
export * from "./sticky-note-icon";
export * from "./sub-brand";
export * from "./suspended-user";
export * from "./teams";
export * from "./transfer-icon";
export * from "./tree-map-icon";
export * from "./updates-icon";
export * from "./user-activity-icon";
export * from "./workspace-icon";
export * from "./workspace";
