/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// pace imports
import { Skeleton } from "@pace/propel/skeleton";
import { cn } from "@pace/utils";

type Props = {
  className?: string;
};

export function PageContentLoader(props: Props) {
  const { className } = props;

  return (
    <div className={cn("relative flex size-full flex-col", className)}>
      {/* header */}
      <div className="relative flex h-12 w-full flex-shrink-0 items-center divide-x divide-subtle border-b border-subtle">
        <Skeleton className="relative flex items-center gap-1 pr-2">
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
        </Skeleton>
        <Skeleton className="relative flex items-center gap-1 px-2">
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
        </Skeleton>
        <Skeleton className="relative flex items-center gap-1 px-2">
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
        </Skeleton>
        <Skeleton className="relative flex items-center gap-1 pl-2">
          <Skeleton.Item width="26px" height="26px" />
          <Skeleton.Item width="26px" height="26px" />
        </Skeleton>
      </div>

      {/* content */}
      <div className="relative flex size-full overflow-hidden pt-[64px]">
        {/* editor loader */}
        <div className="size-full py-5">
          <Skeleton className="relative space-y-4">
            <Skeleton.Item width="50%" height="36px" />
            <div className="space-y-2">
              <div className="py-2">
                <Skeleton.Item width="100%" height="36px" />
              </div>
              <Skeleton.Item width="80%" height="22px" />
              <div className="relative flex items-center gap-2">
                <Skeleton.Item width="30px" height="30px" />
                <Skeleton.Item width="30%" height="22px" />
              </div>
              <div className="py-2">
                <Skeleton.Item width="60%" height="36px" />
              </div>
              <Skeleton.Item width="70%" height="22px" />
              <Skeleton.Item width="30%" height="22px" />
              <div className="relative flex items-center gap-2">
                <Skeleton.Item width="30px" height="30px" />
                <Skeleton.Item width="30%" height="22px" />
              </div>
              <div className="py-2">
                <Skeleton.Item width="50%" height="30px" />
              </div>
              <Skeleton.Item width="100%" height="22px" />
              <div className="py-2">
                <Skeleton.Item width="30%" height="30px" />
              </div>
              <Skeleton.Item width="30%" height="22px" />
              <div className="relative flex items-center gap-2">
                <div className="py-2">
                  <Skeleton.Item width="30px" height="30px" />
                </div>
                <Skeleton.Item width="30%" height="22px" />
              </div>
            </div>
          </Skeleton>
        </div>
      </div>
    </div>
  );
}
