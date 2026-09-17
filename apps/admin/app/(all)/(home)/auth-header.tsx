/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import Link from "@/app/hooks/link";
import { PaceLockup } from "@/components/common/pace-lockup";

export function AuthHeader() {
  return (
    <div className="sticky top-0 flex w-full flex-shrink-0 items-center justify-between gap-6">
      <Link href="/">
        <PaceLockup height={20} width={76} className="text-primary" />
      </Link>
    </div>
  );
}
