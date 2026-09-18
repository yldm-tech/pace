/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// Entry point for `@pace/utils/markdown`, kept out of the package barrel so that the unified/rehype/remark pipeline it depends on is only downloaded by code that converts HTML to Markdown. See ./editor/markdown-parser/index.ts for why.
export * from "./editor/markdown-parser/root";
export * from "./editor/markdown-parser/types";
