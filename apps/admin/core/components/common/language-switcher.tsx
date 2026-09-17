/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useTranslation } from "@pace/i18n";

// The console had no way to change its language at all: it was the one application in the tree that never depended on @pace/i18n, so every string in it was English and there was nothing to switch.
//
// A plain select rather than the command palette web uses. There is no palette here, and a settings console is a place where a visible control beats a discoverable one.
export function LanguageSwitcher() {
  const { t, currentLocale, changeLanguage, languages } = useTranslation();

  return (
    <div className="flex items-center gap-2">
      <label htmlFor="admin-language" className="sr-only">
        {t("admin.language.label")}
      </label>
      <select
        id="admin-language"
        value={currentLocale}
        onChange={(event) => changeLanguage(event.target.value as typeof currentLocale)}
        aria-label={t("admin.language.label")}
        className="bg-surface focus:border-accent-primary rounded-md border border-subtle px-2 py-1 text-13 text-secondary outline-none hover:border-strong"
      >
        {languages.map((language) => (
          <option key={language.value} value={language.value}>
            {language.label}
          </option>
        ))}
      </select>
    </div>
  );
}
