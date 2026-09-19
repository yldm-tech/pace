/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// components
import { Skeleton } from "@pace/propel/skeleton";

export function ProjectDetailsFormLoader() {
  return (
    <>
      <div className="relative mt-6 h-44 w-full">
        <Skeleton>
          <Skeleton.Item height="auto" width="46px" />
        </Skeleton>
        <div className="absolute bottom-4 flex w-full items-end justify-between gap-3 px-4">
          <div className="flex flex-grow gap-3 truncate">
            <div className="flex h-[52px] w-[52px] flex-shrink-0 items-center justify-center rounded-lg bg-surface-2">
              <Skeleton>
                <Skeleton.Item height="46px" width="46px" />
              </Skeleton>
            </div>
          </div>
          <div className="flex flex-shrink-0 justify-center">
            <Skeleton>
              <Skeleton.Item height="32px" width="108px" />
            </Skeleton>
          </div>
        </div>
      </div>
      <div className="my-8 flex flex-col gap-8">
        <div className="flex flex-col gap-1">
          <h4 className="text-13">Project name</h4>
          <Skeleton>
            <Skeleton.Item height="46px" width="100%" />
          </Skeleton>
        </div>
        <div className="flex flex-col gap-1">
          <h4 className="text-13">Description</h4>
          <Skeleton className="w-full">
            <Skeleton.Item height="102px" width="full" />
          </Skeleton>
        </div>
        <div className="flex w-full items-center justify-between gap-10">
          <div className="flex w-1/2 flex-col gap-1">
            <h4 className="text-13">Identifier</h4>
            <Skeleton>
              <Skeleton.Item height="36px" width="100%" />
            </Skeleton>
          </div>
          <div className="flex w-1/2 flex-col gap-1">
            <h4 className="text-13">Network</h4>
            <Skeleton className="w-full">
              <Skeleton.Item height="46px" width="100%" />
            </Skeleton>
          </div>
        </div>
        <div className="flex items-center justify-between py-2">
          <Skeleton className="mt-2 w-full">
            <Skeleton.Item height="34px" width="100px" />
          </Skeleton>
        </div>
      </div>
    </>
  );
}
