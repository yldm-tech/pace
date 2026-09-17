/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { initPromise } from "@pace/i18n";
import { setUnauthorizedHandler } from "@pace/services";
import { startTransition, StrictMode } from "react";
import { hydrateRoot } from "react-dom/client";
import { HydratedRouter } from "react-router/dom";

import polyfills from "@/lib/polyfills";
import { isStaleAssetErrorMessage, recoverFromStaleAsset } from "@/lib/stale-asset-error";
// A 401 from any service sends the person to sign-in and remembers where they were, which is what web's own APIService used to do before that class became the shared one. space and admin install no handler and so are unaffected.
setUnauthorizedHandler((currentPath) => {
  window.location.replace(`/${currentPath ? `?next_path=${currentPath}` : ``}`);
});

void polyfills;

// Production-only: in dev these errors come from the dev server itself (restarts,
// stale optimized deps) and auto-reloading would mask them.
if (import.meta.env.PROD) {
  window.addEventListener("vite:preloadError", (event) => {
    if (recoverFromStaleAsset()) event.preventDefault();
  });

  window.addEventListener("error", (event) => {
    if (isStaleAssetErrorMessage(event.message || "")) recoverFromStaleAsset();
  });

  window.addEventListener("unhandledrejection", (event) => {
    const reason = event.reason instanceof Error ? event.reason.message : String(event.reason ?? "");
    if (isStaleAssetErrorMessage(reason)) recoverFromStaleAsset();
  });
}

// Initialize i18n before hydrating (the remix-i18next pattern for React
// Router: await init, then hydrateRoot). Hydrating before the instance is
// ready would make the first client render diverge from the prerendered
// shell, and React 19 leaves DOM it could not adopt in place instead of
// clearing it.
void initPromise
  .catch(() => {})
  .then(() => {
    startTransition(() => {
      hydrateRoot(
        document,
        <StrictMode>
          <HydratedRouter />
        </StrictMode>
      );
    });
  });
