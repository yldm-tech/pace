/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// types
import { API_BASE_URL } from "@pace/constants";
import type {
  IFormattedInstanceConfiguration,
  IInstance,
  IInstanceAdmin,
  IInstanceConfiguration,
  IInstanceInfo,
  TPage,
} from "@pace/types";
// helpers
// services
import { APIService } from "../api.service";

export class InstanceService extends APIService {
  constructor() {
    super(API_BASE_URL);
  }

  async requestCSRFToken(): Promise<{ csrf_token: string }> {
    return this.get("/auth/get-csrf-token/")
      .then((response) => response.data)
      .catch((error) => {
        throw error;
      });
  }

  async info(): Promise<IInstanceInfo> {
    return this.get("/api/instances/")
      .then((response) => response.data)
      .catch((error) => {
        throw error;
      });
  }

  async changelog(): Promise<TPage> {
    return this.get("/api/instances/changelog/")
      .then((response) => response.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
  async admins(): Promise<IInstanceAdmin[]> {
    return this.get("/api/instances/admins/", { validateStatus: null })
      .then((response) => response.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
  async update(data: Partial<IInstance>): Promise<IInstance> {
    return this.patch("/api/instances/", data)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
  async configurations(): Promise<IInstanceConfiguration[]> {
    return this.get("/api/instances/configurations/")
      .then((response) => response.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
  async updateConfigurations(data: Partial<IFormattedInstanceConfiguration>): Promise<IInstanceConfiguration[]> {
    return this.patch("/api/instances/configurations/", data)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
  async sendTestEmail(receiverEmail: string): Promise<void> {
    return this.post("/api/instances/email-credentials-check/", {
      receiver_email: receiverEmail,
    })
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
  async disableEmail(): Promise<void> {
    return this.delete("/api/instances/configurations/disable-email-feature/")
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
}
