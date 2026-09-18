/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { Skeleton } from "@pace/propel/skeleton";

export function ProjectInsightsLoader() {
  return (
    <div className="flex h-[200px] gap-1">
      <Skeleton className="h-full w-full">
        <Skeleton.Item height="100%" width="100%" />
      </Skeleton>
      <div className="flex h-full w-full flex-col gap-1">
        <Skeleton className="h-12 w-full">
          <Skeleton.Item height="100%" width="100%" />
        </Skeleton>
        <Skeleton className="h-full w-full">
          <Skeleton.Item height="100%" width="100%" />
        </Skeleton>
      </div>
    </div>
  );
}

export function ChartLoader() {
  return (
    <Skeleton className="h-[350px] w-full">
      <Skeleton.Item height="100%" width="100%" />
    </Skeleton>
  );
}
