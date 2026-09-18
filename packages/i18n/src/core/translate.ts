/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { i18nInstance } from "./instance";

/**
 * Translate outside React, for module scope where `useTranslation` cannot run. React Router `meta` exports are why this exists: a `meta` export is a plain module-level function, so a hook is illegal there and the document title would otherwise have to be a hard-coded English literal.
 *
 * `fallback` is required, not optional, because of an ordering hazard. A `meta` function can run before `initPromise` settles and again before the `admin` namespace bundle has finished loading — `i18nInstance` is created eagerly at import time but fills its resource store asynchronously. i18next answers an unresolvable lookup with the key itself, so a naive `t("admin.page.titles.general")` would paint the literal string "admin.page.titles.general" into the browser tab during that window. We therefore accept the lookup only when it comes back as a non-empty string that is not the key, and return `fallback` in every other case: instance not initialised, namespace still loading, or key missing from the locale file.
 *
 * This reads a snapshot, it does not subscribe. Changing the language does not re-run a `meta` export, so a new title lands on the next navigation. Use `useTranslation` anywhere a hook is legal.
 */
export function translate(key: string, fallback: string, params?: Record<string, unknown>): string {
  // `i18nInstance.t` is `this.translator?.translate(...)`, which is `undefined` until `init()` has built the translator.
  if (!i18nInstance.isInitialized) return fallback;
  // `params` is spread first so a caller cannot accidentally shadow `defaultValue`.
  const value = i18nInstance.t(key, { ...params, defaultValue: fallback });
  return typeof value === "string" && value.length > 0 && value !== key ? value : fallback;
}
