/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// issues
export * from "./issue";
export * from "./issue-reaction";
export * from "./issue-link";
export * from "./issue-attachment";
export * from "./issue-relation";
export * from "./issue-sub-issues";
export * from "./activity/base";

export type TLoader = "init-loader" | "mutation" | "pagination" | "loaded" | undefined;

export type TGroupedIssues = {
  [group_id: string]: string[];
};

export type TSubGroupedIssues = {
  [sub_grouped_id: string]: TGroupedIssues;
};

export type TIssues = TGroupedIssues | TSubGroupedIssues;

export type TPaginationData = {
  nextCursor: string;
  prevCursor: string;
  nextPageResults: boolean;
};

export type TIssuePaginationData = {
  [group_id: string]: TPaginationData;
};

export type TGroupedIssueCount = {
  [group_id: string]: number;
};

export type TUnGroupedIssues = string[];
