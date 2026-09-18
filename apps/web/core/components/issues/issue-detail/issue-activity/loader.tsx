/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// pace imports
import { Skeleton } from "@pace/propel/skeleton";

export function IssueActivityLoader() {
  return (
    <Skeleton className="space-y-8">
      <div className="flex items-start gap-3">
        <Skeleton.Item className="shrink-0" height="28px" width="28px" />
        <div className="w-full space-y-2">
          <Skeleton.Item height="8px" width="60%" />
          <Skeleton.Item height="8px" width="40%" />
          <Skeleton.Item height="10px" width="100%" />
        </div>
      </div>
      <div className="flex items-start gap-3">
        <Skeleton.Item className="shrink-0" height="28px" width="28px" />
        <div className="w-full space-y-2">
          <Skeleton.Item height="8px" width="40%" />
          <Skeleton.Item height="8px" width="60%" />
          <Skeleton.Item height="10px" width="80%" />
        </div>
      </div>
      <div className="flex items-start gap-3">
        <Skeleton.Item className="shrink-0" height="28px" width="28px" />
        <div className="w-full space-y-2">
          <Skeleton.Item height="8px" width="60%" />
          <Skeleton.Item height="8px" width="40%" />
          <Skeleton.Item height="10px" width="100%" />
        </div>
      </div>
    </Skeleton>
  );
}
