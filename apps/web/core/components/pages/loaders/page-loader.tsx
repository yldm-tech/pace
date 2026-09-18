/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { range } from "lodash-es";
import { Skeleton } from "@pace/propel/skeleton";

export function PageLoader() {
  return (
    <div className="relative flex h-full w-full flex-col">
      <div className="border-b border-subtle px-3 py-3">
        <Skeleton className="relative flex items-center gap-2">
          <Skeleton.Item width="200px" height="30px" />
          <div className="relative ml-auto flex items-center gap-2">
            <Skeleton.Item width="100px" height="30px" />
            <Skeleton.Item width="100px" height="30px" />
          </div>
        </Skeleton>
      </div>
      <div>
        {range(10).map((i) => (
          <Skeleton key={i} className="relative flex items-center gap-2 border-b border-subtle p-3 py-4">
            <Skeleton.Item width={`${250 + 10 * Math.floor(Math.random() * 10)}px`} height="22px" />
            <div className="relative ml-auto flex items-center gap-2">
              <Skeleton.Item width="60px" height="22px" />
              <Skeleton.Item width="22px" height="22px" />
              <Skeleton.Item width="22px" height="22px" />
              <Skeleton.Item width="22px" height="22px" />
            </div>
          </Skeleton>
        ))}
      </div>
    </div>
  );
}
