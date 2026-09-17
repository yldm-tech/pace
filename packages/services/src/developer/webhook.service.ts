/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

// api services
import { API_BASE_URL } from "@pace/constants";
import type { IWebhook } from "@pace/types";
import { APIService } from "../api.service";
// helpers
// types

export class WebhookService extends APIService {
  constructor() {
    super(API_BASE_URL);
  }

  async list(workspaceSlug: string): Promise<IWebhook[]> {
    return this.get(`/api/workspaces/${workspaceSlug}/webhooks/`)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }

  async retrieve(workspaceSlug: string, webhookId: string): Promise<IWebhook> {
    return this.get(`/api/workspaces/${workspaceSlug}/webhooks/${webhookId}/`)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }

  async create(workspaceSlug: string, data = {}): Promise<IWebhook> {
    return this.post(`/api/workspaces/${workspaceSlug}/webhooks/`, data)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }

  async update(workspaceSlug: string, webhookId: string, data = {}): Promise<IWebhook> {
    return this.patch(`/api/workspaces/${workspaceSlug}/webhooks/${webhookId}/`, data)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }

  async destroy(workspaceSlug: string, webhookId: string): Promise<void> {
    return this.delete(`/api/workspaces/${workspaceSlug}/webhooks/${webhookId}/`)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }

  async regenerateSecretKey(workspaceSlug: string, webhookId: string): Promise<IWebhook> {
    return this.post(`/api/workspaces/${workspaceSlug}/webhooks/${webhookId}/regenerate/`)
      .then((response) => response?.data)
      .catch((error) => {
        throw error?.response?.data;
      });
  }
}
