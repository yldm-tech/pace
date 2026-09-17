/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { ThemeProvider } from "next-themes";
import { SWRConfig } from "swr";
import { TranslationProvider } from "@pace/i18n";
import { AppProgressBar } from "@pace/ui";
// local imports
import { ToastWithTheme } from "./toast";
import { StoreProvider } from "./store.provider";
import { InstanceProvider } from "./instance.provider";
import { UserProvider } from "./user.provider";

const DEFAULT_SWR_CONFIG = {
  refreshWhenHidden: false,
  revalidateIfStale: false,
  revalidateOnFocus: false,
  revalidateOnMount: true,
  refreshInterval: 600_000,
  errorRetryCount: 3,
};

export function CoreProviders({ children }: { children: React.ReactNode }) {
  return (
    <ThemeProvider themes={["light", "dark"]} defaultTheme="system" enableSystem>
      <AppProgressBar />
      <ToastWithTheme />
      {/* Inside the theme provider and outside the stores, which is where web puts it: the stores do not read translations, and a toast raised during startup should already be able to. */}
      <TranslationProvider>
        <SWRConfig value={DEFAULT_SWR_CONFIG}>
          <StoreProvider>
            <InstanceProvider>
              <UserProvider>{children}</UserProvider>
            </InstanceProvider>
          </StoreProvider>
        </SWRConfig>
      </TranslationProvider>
    </ThemeProvider>
  );
}
