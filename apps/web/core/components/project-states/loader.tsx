/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { Skeleton } from "@pace/propel/skeleton";

export function ProjectStateLoader() {
  return (
    <Skeleton className="space-y-5 md:w-2/3">
      <Skeleton.Item height="40px" />
      <Skeleton.Item height="40px" />
      <Skeleton.Item height="40px" />
      <Skeleton.Item height="40px" />
    </Skeleton>
  );
}
