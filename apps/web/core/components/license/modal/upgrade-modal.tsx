/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
// pace imports
import {
  BUSINESS_PLAN_FEATURES,
  ENTERPRISE_PLAN_FEATURES,
  PLANE_COMMUNITY_PRODUCTS,
  PRO_PLAN_FEATURES,
  SUBSCRIPTION_WEBPAGE_URLS,
} from "@pace/constants";
import { EProductSubscriptionEnum } from "@pace/types";
import { EModalWidth, ModalCore } from "@pace/ui";
import { cn } from "@pace/utils";
// components
import { FreePlanCard, PlanUpgradeCard } from "@/components/license";

// Constants
const COMMON_CARD_CLASSNAME = "flex flex-col w-full h-full justify-end col-span-12 sm:col-span-6 xl:col-span-3";
const COMMON_EXTRA_FEATURES_CLASSNAME = "pt-2 text-center text-caption-md-medium text-accent-primary hover:underline";

// The plan pages live on a marketing site that a self-hosted installation may not have, so the link is omitted rather than rendered with an empty href.
const renderFullFeaturesLink = (planVariant: EProductSubscriptionEnum) => {
  const href = SUBSCRIPTION_WEBPAGE_URLS[planVariant];
  if (!href) return null;
  return (
    <p className={COMMON_EXTRA_FEATURES_CLASSNAME}>
      <a href={href} target="_blank" rel="noreferrer">
        See full features list
      </a>
    </p>
  );
};

export type PaidPlanUpgradeModalProps = {
  isOpen: boolean;
  handleClose: () => void;
};

export const PaidPlanUpgradeModal = observer(function PaidPlanUpgradeModal(props: PaidPlanUpgradeModalProps) {
  const { isOpen, handleClose } = props;
  // derived values
  const isSelfHosted = true;
  const isTrialAllowed = false;

  return (
    <ModalCore isOpen={isOpen} handleClose={handleClose} width={EModalWidth.VIIXL} className="rounded-2xl">
      <div className="max-h-[90vh] overflow-auto p-10">
        <div className="grid h-full grid-cols-12 gap-6">
          {/* Free Plan Section */}
          <div className={cn(COMMON_CARD_CLASSNAME)}>
            <div className="flex text-24 leading-8 font-bold">Upgrade to a paid plan and unlock missing features.</div>
            <div className="mt-4 mb-2">
              <p className="mb-4 pr-8 text-13 text-primary">
                Dashboards, Workflows, Approvals, Time Management, and other superpowers are just a click away. Upgrade
                today to unlock features your teams need yesterday.
              </p>
            </div>

            {/* Free plan details */}
            <FreePlanCard isOnFreePlan />
          </div>

          {/* Pro plan */}
          <div className={cn(COMMON_CARD_CLASSNAME)}>
            <PlanUpgradeCard
              planVariant={EProductSubscriptionEnum.PRO}
              product={PLANE_COMMUNITY_PRODUCTS[EProductSubscriptionEnum.PRO]}
              features={PRO_PLAN_FEATURES}
              verticalFeatureList
              extraFeatures={renderFullFeaturesLink(EProductSubscriptionEnum.PRO)}
              isSelfHosted={!!isSelfHosted}
              isTrialAllowed={!!isTrialAllowed}
            />
          </div>
          <div className={cn(COMMON_CARD_CLASSNAME)}>
            <PlanUpgradeCard
              planVariant={EProductSubscriptionEnum.BUSINESS}
              product={PLANE_COMMUNITY_PRODUCTS[EProductSubscriptionEnum.BUSINESS]}
              features={BUSINESS_PLAN_FEATURES}
              verticalFeatureList
              extraFeatures={renderFullFeaturesLink(EProductSubscriptionEnum.BUSINESS)}
              isSelfHosted={!!isSelfHosted}
              isTrialAllowed={!!isTrialAllowed}
            />
          </div>
          <div className={cn(COMMON_CARD_CLASSNAME)}>
            <PlanUpgradeCard
              planVariant={EProductSubscriptionEnum.ENTERPRISE}
              product={PLANE_COMMUNITY_PRODUCTS[EProductSubscriptionEnum.ENTERPRISE]}
              features={ENTERPRISE_PLAN_FEATURES}
              verticalFeatureList
              extraFeatures={renderFullFeaturesLink(EProductSubscriptionEnum.ENTERPRISE)}
              isSelfHosted={!!isSelfHosted}
              isTrialAllowed={!!isTrialAllowed}
            />
          </div>
        </div>
      </div>
    </ModalCore>
  );
});
