/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { API_BASE_URL } from "@pace/constants";
import type { TTimezones } from "@pace/types";
// helpers
// api services
import { APIService } from "../api.service";

export class TimezoneService extends APIService {
  constructor() {
    super(API_BASE_URL);
  }

  async fetch(): Promise<TTimezones> {
    return this.get(`/api/timezones/`)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
}

export const timezoneService = new TimezoneService();

export default timezoneService;
