/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { PRIVACY_POLICY_URL, TERMS_OF_SERVICE_URL } from "@pace/constants";

type Props = {
  isSignUp?: boolean;
};

export function TermsAndConditions(props: Props) {
  const { isSignUp = false } = props;
  // Nothing is claimed when this installation publishes neither document. Telling someone they agreed to terms that are not reachable is worse than saying nothing, and the previous links pointed at paths this deployment does not serve.
  if (!TERMS_OF_SERVICE_URL || !PRIVACY_POLICY_URL) return null;

  return (
    <span className="flex items-center justify-center py-6">
      <p className="text-center text-13 whitespace-pre-line text-secondary">
        {isSignUp ? "By creating an account" : "By signing in"}, you agree to our{" \n"}
        <a href={TERMS_OF_SERVICE_URL} target="_blank" rel="noopener noreferrer">
          <span className="text-13 font-medium underline hover:cursor-pointer">Terms of Service</span>
        </a>{" "}
        and{" "}
        <a href={PRIVACY_POLICY_URL} target="_blank" rel="noopener noreferrer">
          <span className="text-13 font-medium underline hover:cursor-pointer">Privacy Policy</span>
        </a>
        {"."}
      </p>
    </span>
  );
}
