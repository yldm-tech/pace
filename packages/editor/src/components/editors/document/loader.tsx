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

export function DocumentContentLoader(props: Props) {
  const { className } = props;

  return (
    <div className={cn("document-editor-loader", className)}>
      <Skeleton className="relative space-y-4">
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
  );
}
