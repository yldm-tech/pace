/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { CHANGELOG_URL } from "@pace/constants";
import { EmptyStateDetailed } from "@pace/propel/empty-state";

type TProductUpdatesFallbackProps = {
  description: string;
  variant: "cloud" | "self-managed";
};

export function ProductUpdatesFallback(props: TProductUpdatesFallbackProps) {
  const { description, variant } = props;
  // derived values
  // The empty state stays either way, since it is what this modal shows when the updates feed could not be read. Only the button goes when there is no changelog to send anybody to: this screen is already an apology, and a second dead end on top of it is worse than none.
  const changelogUrl = CHANGELOG_URL
    ? `${CHANGELOG_URL}?category=${variant === "cloud" ? "cloud" : "self-hosted"}`
    : "";

  return (
    <div className="py-8">
      <EmptyStateDetailed
        assetKey="changelog"
        description={description}
        align="center"
        actions={
          changelogUrl
            ? [
                {
                  label: "Go to changelog",
                  variant: "primary",
                  onClick: () => window.open(changelogUrl, "_blank"),
                },
              ]
            : []
        }
      />
    </div>
  );
}
