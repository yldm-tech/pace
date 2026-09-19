/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import ICU from "i18next-icu";
import resourcesToBackend from "i18next-resources-to-backend";
import { SUPPORTED_LANGUAGES, FALLBACK_LANGUAGE, LANGUAGE_STORAGE_KEY } from "../constants/language";
import { NAMESPACES, DEFAULT_NAMESPACE } from "../constants/namespaces";

import type { i18n as I18nInstance } from "i18next";
import type { TNamespace } from "../constants/namespaces";

declare const __PACE_I18N_EAGER_NAMESPACES__: readonly TNamespace[] | undefined;

// Which namespaces to fetch before the first render. An app narrows this through a Vite `define` (see apps/admin/vite.config.ts); anything that does not set it -- web, space, the test runner, the sync-check script -- gets every namespace, which is the behaviour this had before the define existed.
const EAGER_NAMESPACES: readonly TNamespace[] =
  typeof __PACE_I18N_EAGER_NAMESPACES__ !== "undefined" ? __PACE_I18N_EAGER_NAMESPACES__ : NAMESPACES;

export const i18nInstance: I18nInstance = i18n.createInstance();

// The plugin registration and the `init` below run at import time, which is the whole contract of this module: importing @pace/i18n anywhere is what configures the instance. That is why packages/i18n deliberately has no `sideEffects: false` in its package.json, unlike the pure-data packages around it -- marking it pure would let a bundler drop this module when a consumer only reads a re-exported constant from the barrel, and the failure would be an unconfigured i18next at runtime rather than a build error.
//
// `../locales` resolves through a symlink that is committed to git, and the build depends on it. tsdown does not rewrite this template specifier, so the emitted dist/index.js contains it verbatim and the app's bundler resolves it relative to dist/ -- that is packages/i18n/locales, not the src/locales where the JSON actually lives. What bridges the two is `packages/i18n/locales -> src/locales`, a mode 120000 entry in the index since the first commit (`git ls-files -s packages/i18n/locales`). Nothing copies the JSON into dist, and package.json exports no `./locales/*`, so those 600 files enter the bundle graph through a path the manifest does not describe.
//
// Consequences worth knowing before touching this line, the tsdown entry layout, or the `../` depth: a clone without symlink support (Windows without `core.symlinks`) and any change that alters how deep dist/index.js sits both break every namespace fetch, and i18next answers an unresolvable lookup with the key itself -- so the symptom is raw `issue.title` strings painted into a UI that never logs an error, the same failure mode ./translate.ts documents at length for its `meta` exports.
i18nInstance
  .use(ICU)
  .use(initReactI18next)
  .use(resourcesToBackend((language: string, namespace: string) => import(`../locales/${language}/${namespace}.json`)));

const initialLng =
  typeof window !== "undefined" ? localStorage.getItem(LANGUAGE_STORAGE_KEY) || FALLBACK_LANGUAGE : FALLBACK_LANGUAGE;

export const initPromise = i18nInstance
  .init({
    lng: initialLng,
    fallbackLng: FALLBACK_LANGUAGE,
    supportedLngs: SUPPORTED_LANGUAGES.map((l) => l.value),
    ns: [...EAGER_NAMESPACES],
    defaultNS: DEFAULT_NAMESPACE,
    // fallbackNS ensures all namespaces are searched for any key, so components don't need to pass NAMESPACES to useTranslation (which triggers re-render cascades). It searches loaded bundles only, so it is scoped to what is actually fetched.
    fallbackNS: EAGER_NAMESPACES.filter((ns) => ns !== DEFAULT_NAMESPACE),
    partialBundledLanguages: true,
    keySeparator: ".",
    nsSeparator: false,
    interpolation: { escapeValue: false },
    returnNull: false,
    returnEmptyString: false,
    // Pinned explicitly even though it's the default — i18next-icu intercepts the
    // format pipeline and returns raw objects regardless of this flag, so the runtime
    // guard in useTranslation is what actually prevents React crashes. Documenting
    // intent here so this isn't accidentally flipped.
    returnObjects: false,
    react: { useSuspense: false },
  })
  // Eagerly pre-load the app's namespaces for the initial language so they're cached before any component renders. This prevents the re-render cascade that occurs when react-i18next triggers concurrent async loads for unloaded namespaces.
  .then(() => i18nInstance.loadNamespaces([...EAGER_NAMESPACES]));
