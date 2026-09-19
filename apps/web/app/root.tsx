/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import type { ReactNode } from "react";
import { Links, Meta, Outlet, Scripts } from "react-router";
import type { LinksFunction } from "react-router";
import { ThemeProvider, useTheme } from "next-themes";
// pace imports
import { SITE_DESCRIPTION, SITE_NAME } from "@pace/constants";
// types
// assets
import favicon16 from "@/assets/favicon/favicon-16x16.png?url";
import favicon32 from "@/assets/favicon/favicon-32x32.png?url";
import faviconIco from "@/assets/favicon/favicon.ico?url";
import icon180 from "@/assets/icons/icon-180x180.png?url";
import icon512 from "@/assets/icons/icon-512x512.png?url";
import ogImage from "@/assets/og-image.png?url";
import globalStyles from "@/styles/globals.css?url";
import type { Route } from "./+types/root";
// lib
import { isStaleAssetError, recoverFromStaleAsset } from "@/lib/stale-asset-error";
// local
import { CustomErrorComponent } from "./error";
// fonts
import "@fontsource-variable/inter";
import interVariableWoff2 from "@fontsource-variable/inter/files/inter-latin-wght-normal.woff2?url";
// Weight 400 only: the `.material-symbols-rounded` rule pins font-weight 400, so the package's index.css would add three more unusable @font-face blocks (100/200/300) to a render-blocking stylesheet.
import "@fontsource/material-symbols-rounded/400.css";
import "@fontsource/ibm-plex-mono";

const APP_TITLE = "Pace | Simple, extensible, open-source project management tool.";

export const links: LinksFunction = () => [
  { rel: "icon", type: "image/png", sizes: "32x32", href: favicon32 },
  { rel: "icon", type: "image/png", sizes: "16x16", href: favicon16 },
  { rel: "shortcut icon", href: faviconIco },
  { rel: "manifest", href: "/site.webmanifest.json" },
  { rel: "apple-touch-icon", href: icon512 },
  { rel: "apple-touch-icon", sizes: "180x180", href: icon180 },
  { rel: "apple-touch-icon", sizes: "512x512", href: icon512 },
  { rel: "stylesheet", href: globalStyles },
  {
    rel: "preload",
    href: interVariableWoff2,
    as: "font",
    type: "font/woff2",
    crossOrigin: "anonymous",
  },
];

export function Layout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <meta charSet="utf-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1" />
        <meta name="theme-color" content="#fff" />
        {/* Meta info for PWA */}
        <meta name="application-name" content="Pace" />
        <meta name="apple-mobile-web-app-capable" content="yes" />
        <meta name="apple-mobile-web-app-status-bar-style" content="default" />
        <meta name="apple-mobile-web-app-title" content={SITE_NAME} />
        <meta name="format-detection" content="telephone=no" />
        <meta name="mobile-web-app-capable" content="yes" />
        <Meta />
        <Links />
      </head>
      <body suppressHydrationWarning>
        <div id="context-menu-portal" />
        <div id="editor-portal" />
        <ThemeProvider themes={["light", "dark", "light-contrast", "dark-contrast", "custom"]} defaultTheme="system">
          {children}
        </ThemeProvider>
        <Scripts />
      </body>
    </html>
  );
}

export const meta: Route.MetaFunction = () => [
  { title: APP_TITLE },
  { name: "description", content: SITE_DESCRIPTION },
  { property: "og:title", content: APP_TITLE },
  {
    property: "og:description",
    content: "Open-source project management tool to manage work items, cycles, and product roadmaps easily",
  },
  { property: "og:url", content: "https://pace.yldm.ai/" },
  { property: "og:image", content: ogImage },
  { property: "og:image:width", content: "1200" },
  { property: "og:image:height", content: "630" },
  { property: "og:image:alt", content: "Pace - Modern project management" },
  {
    name: "keywords",
    content:
      "software development, plan, ship, software, accelerate, code management, release management, project management, work item tracking, agile, scrum, kanban, collaboration",
  },
  { name: "twitter:card", content: "summary_large_image" },
  { name: "twitter:image", content: ogImage },
  { name: "twitter:image:width", content: "1200" },
  { name: "twitter:image:height", content: "630" },
  { name: "twitter:image:alt", content: "Pace - Modern project management" },
];

// Root stays shell-thin: in SPA mode React Router server-builds only the root route, so
// everything imported here is evaluated in Node just to prerender the fallback index.html.
// Providers, the store layer, and app chrome belong in app/layout.tsx — never import them here.
export default function Root() {
  return <Outlet />;
}

export function HydrateFallback() {
  const { resolvedTheme } = useTheme();

  // if we are on the server or the theme is not resolved, return an empty div
  if (typeof window === "undefined" || resolvedTheme === undefined) return <div />;

  return (
    <div className="relative flex h-screen w-full items-center justify-center bg-canvas">
      {/* The pre-hydration spinner is the brand mark (packages/brand/mark.svg) drawn inline rather than the themed GIF pair LogoSpinner uses: root is the first chunk the browser runs, so anything it imports is fetched before a single route module, and the dark GIF alone is 953 KB. Inline costs no request, and currentColor tracks the theme the way the two files did. */}
      <svg
        viewBox="0 0 46 64"
        fill="currentColor"
        role="img"
        aria-label="Loading"
        className="h-6 w-auto animate-pulse text-primary sm:h-11"
      >
        <path
          fillRule="evenodd"
          transform="translate(2 0) skewX(-8)"
          d="M8 0H24A20 20 0 0 1 24 40V64H8Z M24 12A8 8 0 0 1 24 28Z"
        />
      </svg>
    </div>
  );
}

export function ErrorBoundary({ error }: Route.ErrorBoundaryProps) {
  // A stale chunk failure surfaces here as React Router's own wrapper error
  // (the failed dynamic import itself never reaches a window event) — recover
  // the same way entry.client.tsx does instead of just showing the error page.
  if (import.meta.env.PROD && isStaleAssetError(error)) recoverFromStaleAsset();

  return <CustomErrorComponent error={error} />;
}
