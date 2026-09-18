/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useMemo } from "react";
import { useLocation, useNavigate, useParams as useParamsRR, useSearchParams as useSearchParamsRR } from "react-router";
import { ensureTrailingSlash } from "@pace/utils";

export function useRouter() {
  const navigate = useNavigate();
  return useMemo(
    () => ({
      push: (to: string) => {
        // Defer navigation to avoid state updates during render
        setTimeout(() => navigate(ensureTrailingSlash(to)), 0);
      },
      replace: (to: string) => {
        // Defer navigation to avoid state updates during render
        setTimeout(() => navigate(ensureTrailingSlash(to), { replace: true }), 0);
      },
      back: () => {
        setTimeout(() => navigate(-1), 0);
      },
      forward: () => {
        setTimeout(() => navigate(1), 0);
      },
      refresh: () => {
        location.reload();
      },
      prefetch: async (_to: string) => {
        // no-op in this shim
      },
    }),
    [navigate]
  );
}

export function usePathname(): string {
  const { pathname } = useLocation();
  return pathname;
}

export function useSearchParams(): URLSearchParams {
  const [searchParams] = useSearchParamsRR();
  return searchParams;
}

// useParams reads the route parameters, with the values typed as present.
//
// react-router types every parameter as `string | undefined`, because as far as its type is concerned any of them may be missing. A component here renders under a route that names the ones it reads, so the cast says what the route already guarantees. It used to live in app/types/next-navigation.d.ts, where it silently contradicted what this function returns; it is visible now.
export function useParams<T extends Record<string, string> = Record<string, string>>(): T {
  return useParamsRR() as T;
}
