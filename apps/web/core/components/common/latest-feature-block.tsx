/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import Link from "@/app/hooks/link";
import { useTheme } from "next-themes";
// pace imports
import { CHANGELOG_URL } from "@pace/constants";
// icons
import { ThoughtsOutline } from "@makeplane/propel/icons";
// images
import latestFeatures from "@/app/assets/onboarding/onboarding-pages.webp?url";

export function LatestFeatureBlock() {
  const { resolvedTheme } = useTheme();

  return (
    <>
      <div className="mx-auto mt-16 flex rounded-[3.5px] border border-subtle bg-surface-1 py-2 sm:w-96">
        <ThoughtsOutline className="mx-3 mr-2 h-7 w-7" />
        <p className="text-left text-13 text-primary">
          Pages gets a facelift! Write anything and use Galileo to help you start.
          {/* Only the "Learn more" link goes when this installation publishes no changelog. The sentence in front of it is the announcement itself and stands on its own, so there is no reason to blank the whole block -- and the block is decoration on the sign-in screen, where a link to nowhere would be the first thing a new user clicks. */}
          {CHANGELOG_URL && (
            <>
              {" "}
              <Link href={CHANGELOG_URL} target="_blank" rel="noopener noreferrer">
                <span className="text-13 font-medium underline hover:cursor-pointer">Learn more</span>
              </Link>
            </>
          )}
        </p>
      </div>
      <div
        className={`mx-auto mt-8 overflow-hidden rounded-md border border-subtle object-cover sm:h-52 sm:w-96 ${
          resolvedTheme === "dark" ? "bg-surface-1" : "bg-layer-2"
        }`}
      >
        <div className="h-[90%]">
          <img
            src={latestFeatures}
            alt="Pace work items"
            className={`-mt-2 ml-10 h-full rounded-md ${resolvedTheme === "dark" ? "bg-surface-1" : "bg-layer-2"}`}
          />
        </div>
      </div>
    </>
  );
}
