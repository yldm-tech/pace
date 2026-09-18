/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// Only the types belong in the package barrel. `./root` pulls in unified, rehype-parse, rehype-remark, remark-gfm and remark-stringify as static top-level imports, and because the barrel is a single bundled module, every consumer of any utility in this package would then have that entire HTML-to-Markdown pipeline in its first load -- including apps that never mount an editor. It lives behind the `@pace/utils/markdown` subpath instead, so the cost is paid by the editor chunk that actually calls it.
export * from "./types";
