/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { range } from "lodash-es";
// ui
import { Skeleton } from "@pace/propel/skeleton";

export function QuickLinksWidgetLoader() {
  return (
    <Skeleton className="flex flex-wrap gap-2 rounded-xl bg-surface-1">
      {range(4).map((index) => (
        <Skeleton.Item key={index} height="56px" width="230px" />
      ))}
    </Skeleton>
  );
}
