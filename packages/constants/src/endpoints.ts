/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

export const API_BASE_URL = process.env.VITE_API_BASE_URL || "";
export const API_BASE_PATH = process.env.VITE_API_BASE_PATH || "";
export const API_URL = encodeURI(`${API_BASE_URL}${API_BASE_PATH}`);
// God Mode Admin App Base Url
export const ADMIN_BASE_URL = process.env.VITE_ADMIN_BASE_URL || "";
export const ADMIN_BASE_PATH = process.env.VITE_ADMIN_BASE_PATH || "";
export const ADMIN_URL = encodeURI(`${ADMIN_BASE_URL}${ADMIN_BASE_PATH}`);
// Publish App Base Url
export const SPACE_BASE_URL = process.env.VITE_SPACE_BASE_URL || "";
export const SPACE_BASE_PATH = process.env.VITE_SPACE_BASE_PATH || "";
export const SITES_URL = encodeURI(`${SPACE_BASE_URL}${SPACE_BASE_PATH}`);
// Live App Base Url
export const LIVE_BASE_URL = process.env.VITE_LIVE_BASE_URL || "";
export const LIVE_BASE_PATH = process.env.VITE_LIVE_BASE_PATH || "";
export const LIVE_URL = encodeURI(`${LIVE_BASE_URL}${LIVE_BASE_PATH}`);
// Web App Base Url
export const WEB_BASE_URL = process.env.VITE_WEB_BASE_URL || "";
export const WEB_BASE_PATH = process.env.VITE_WEB_BASE_PATH || "";
export const WEB_URL = encodeURI(`${WEB_BASE_URL}${WEB_BASE_PATH}`);
// This installation's own website, which is where "powered by" and the like point. The app's own public home is a real destination, so this one keeps a default.
export const WEBSITE_URL = process.env.VITE_WEBSITE_URL || "https://pace.yldm.ai";
// Whoever runs this installation, not upstream. Left empty rather than guessed: every message that uses it already falls back to "administrator" when it is, and mailing support@yldm.ai about an account on someone else's fork helps nobody. Set VITE_SUPPORT_EMAIL to fill it in.
export const SUPPORT_EMAIL = process.env.VITE_SUPPORT_EMAIL || "";
// The four hosts below are separate sites upstream -- a marketing site, docs.plane.so, forum.plane.so and a status page -- and this fork has no equivalent of any of them at a known address. They are empty by default for the same reason SUPPORT_EMAIL is, and the rule for using one is the same: when it is empty, do not render the link at all. Do NOT fall back to WEBSITE_URL. That is exactly what the domain rename did, and because the web app's root route is the dynamic `:workspaceSlug` segment, a marketing path sent to the app's own domain does not even land on a router 404 -- `/pricing` is read as a workspace named "pricing" and fails as a missing workspace, while a deeper path like `/upgrade/pro/self-hosted` matches nothing and 404s outright.
export const MARKETING_SITE_URL = process.env.VITE_MARKETING_SITE_URL || "";
export const DOCS_URL = process.env.VITE_DOCS_URL || "";
export const FORUM_URL = process.env.VITE_FORUM_URL || "";
export const STATUS_URL = process.env.VITE_STATUS_URL || "";

/**
 * Builds a link to a page on one of the optional external sites above, and returns "" when that site is not configured.
 *
 * Callers must treat "" as "there is no such page here" and omit the link, rather than rendering an anchor with an empty href -- which navigates to the current page and reads as a broken control.
 */
export const externalLink = (siteUrl: string, path = ""): string =>
  siteUrl ? `${siteUrl.replace(/\/$/, "")}${path}` : "";

// This installation's own terms and privacy policy. They default to the marketing site's conventional paths when one is configured, and are overridable on their own because legal documents are often published somewhere else entirely.
//
// When these are empty the sign-in and sign-up screens drop the whole "you understand and agree to our Terms of Service and Privacy Policy" sentence rather than keeping the claim and dropping the links: an installation that publishes no terms should not be telling people they agreed to them.
export const TERMS_OF_SERVICE_URL =
  process.env.VITE_TERMS_OF_SERVICE_URL || externalLink(MARKETING_SITE_URL, "/legals/terms-and-conditions");
export const PRIVACY_POLICY_URL =
  process.env.VITE_PRIVACY_POLICY_URL || externalLink(MARKETING_SITE_URL, "/legals/privacy-policy");

// marketing links, empty unless VITE_MARKETING_SITE_URL is set
export const MARKETING_PRICING_PAGE_LINK = externalLink(MARKETING_SITE_URL, "/pricing");
export const MARKETING_CONTACT_US_PAGE_LINK = externalLink(MARKETING_SITE_URL, "/contact");
export const MARKETING_PLANE_ONE_PAGE_LINK = externalLink(MARKETING_SITE_URL, "/one");
export const MARKETING_PAGES_PAGE_LINK = externalLink(MARKETING_SITE_URL, "/pages");
export const CHANGELOG_URL = externalLink(MARKETING_SITE_URL, "/changelog");
