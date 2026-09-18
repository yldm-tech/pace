/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import type { IPaymentProduct, TProductBillingFrequency } from "@pace/types";
import { EProductSubscriptionEnum } from "@pace/types";
// local imports
import { MARKETING_SITE_URL, externalLink } from "./endpoints";

/**
 * Default billing frequency for each product subscription type
 */
export const DEFAULT_PRODUCT_BILLING_FREQUENCY: TProductBillingFrequency = {
  [EProductSubscriptionEnum.FREE]: undefined,
  [EProductSubscriptionEnum.ONE]: undefined,
  [EProductSubscriptionEnum.PRO]: "month",
  [EProductSubscriptionEnum.BUSINESS]: "month",
  [EProductSubscriptionEnum.ENTERPRISE]: "month",
};

/**
 * Subscription types that support billing frequency toggle (monthly/yearly)
 */
export const SUBSCRIPTION_WITH_BILLING_FREQUENCY = [
  EProductSubscriptionEnum.PRO,
  EProductSubscriptionEnum.BUSINESS,
  EProductSubscriptionEnum.ENTERPRISE,
];

/**
 * Mapping of product subscription types to their respective payment product details
 * Used to provide information about each product's pricing and features
 */
export const PLANE_COMMUNITY_PRODUCTS: Record<string, IPaymentProduct> = {
  [EProductSubscriptionEnum.PRO]: {
    id: EProductSubscriptionEnum.PRO,
    name: "Pace Pro",
    description:
      "More views, more cycles powers, more pages features, new reports, and better dashboards are waiting to be unlocked.",
    type: "PRO",
    prices: [
      {
        id: `price_monthly_${EProductSubscriptionEnum.PRO}`,
        unit_amount: 800,
        recurring: "month",
        currency: "usd",
        workspace_amount: 800,
        product: EProductSubscriptionEnum.PRO,
      },
      {
        id: `price_yearly_${EProductSubscriptionEnum.PRO}`,
        unit_amount: 7200,
        recurring: "year",
        currency: "usd",
        workspace_amount: 7200,
        product: EProductSubscriptionEnum.PRO,
      },
    ],
    payment_quantity: 1,
    is_active: true,
  },
  [EProductSubscriptionEnum.BUSINESS]: {
    id: EProductSubscriptionEnum.BUSINESS,
    name: "Pace Business",
    description:
      "The earliest packaging of Business at $10 a seat a month billed annually, $12 a seat a month billed monthly for Pace Cloud",
    type: "BUSINESS",
    prices: [
      {
        id: `price_yearly_${EProductSubscriptionEnum.BUSINESS}`,
        unit_amount: 15600,
        recurring: "year",
        currency: "usd",
        workspace_amount: 15600,
        product: EProductSubscriptionEnum.BUSINESS,
      },
      {
        id: `price_monthly_${EProductSubscriptionEnum.BUSINESS}`,
        unit_amount: 1500,
        recurring: "month",
        currency: "usd",
        workspace_amount: 1500,
        product: EProductSubscriptionEnum.BUSINESS,
      },
    ],
    payment_quantity: 1,
    is_active: true,
  },
  [EProductSubscriptionEnum.ENTERPRISE]: {
    id: EProductSubscriptionEnum.ENTERPRISE,
    name: "Pace Enterprise",
    description: "",
    type: "ENTERPRISE",
    prices: [
      {
        id: `price_yearly_${EProductSubscriptionEnum.ENTERPRISE}`,
        unit_amount: 0,
        recurring: "year",
        currency: "usd",
        workspace_amount: 0,
        product: EProductSubscriptionEnum.ENTERPRISE,
      },
      {
        id: `price_monthly_${EProductSubscriptionEnum.ENTERPRISE}`,
        unit_amount: 0,
        recurring: "month",
        currency: "usd",
        workspace_amount: 0,
        product: EProductSubscriptionEnum.ENTERPRISE,
      },
    ],
    payment_quantity: 1,
    is_active: false,
  },
};

/**
 * URL for the "Talk to Sales" page where users can contact sales team.
 *
 * Empty unless VITE_MARKETING_SITE_URL is configured, and every caller must omit its link when it is — see the comment on MARKETING_SITE_URL in endpoints.ts.
 */
export const TALK_TO_SALES_URL = externalLink(MARKETING_SITE_URL, "/talk-to-sales");

/**
 * Mapping of subscription types to their respective marketing webpage URLs
 * Used to direct users to learn more about each plan's features and pricing
 *
 * There is deliberately no counterpart for the paid upgrade flow. Upstream, a self-hosted installation sent buyers to the vendor's own cloud app (app.plane.so/upgrade/...) to buy a licence, and this fork has no such service -- so a checkout redirect here has nowhere to go and the CTA that used it is gone rather than pointing somewhere that 404s.
 */
export const SUBSCRIPTION_WEBPAGE_URLS: Record<EProductSubscriptionEnum, string> = {
  [EProductSubscriptionEnum.FREE]: TALK_TO_SALES_URL,
  [EProductSubscriptionEnum.ONE]: TALK_TO_SALES_URL,
  [EProductSubscriptionEnum.PRO]: externalLink(MARKETING_SITE_URL, "/pro"),
  [EProductSubscriptionEnum.BUSINESS]: externalLink(MARKETING_SITE_URL, "/business"),
  [EProductSubscriptionEnum.ENTERPRISE]: externalLink(MARKETING_SITE_URL, "/business"),
};
