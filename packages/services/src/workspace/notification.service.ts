/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

/* eslint-disable no-useless-catch */

import { API_BASE_URL } from "@pace/constants";
import type {
  TNotificationPaginatedInfo,
  TNotificationPaginatedInfoQueryParams,
  TNotification,
  TUnreadNotificationsCount,
} from "@pace/types";
// helpers
// services
import { APIService } from "../api.service";

export class WorkspaceNotificationService extends APIService {
  constructor() {
    super(API_BASE_URL);
  }

  async getUnreadCount(workspaceSlug: string): Promise<TUnreadNotificationsCount | undefined> {
    try {
      const { data } = await this.get(`/api/workspaces/${workspaceSlug}/users/notifications/unread/`);
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async list(
    workspaceSlug: string,
    params: TNotificationPaginatedInfoQueryParams
  ): Promise<TNotificationPaginatedInfo | undefined> {
    try {
      const { data } = await this.get(`/api/workspaces/${workspaceSlug}/users/notifications/`, {
        params,
      });
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async update(
    workspaceSlug: string,
    notificationId: string,
    payload: Partial<TNotification>
  ): Promise<TNotification | undefined> {
    try {
      const { data } = await this.patch(
        `/api/workspaces/${workspaceSlug}/users/notifications/${notificationId}/`,
        payload
      );
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async markAsRead(workspaceSlug: string, notificationId: string): Promise<TNotification | undefined> {
    try {
      const { data } = await this.post(`/api/workspaces/${workspaceSlug}/users/notifications/${notificationId}/read/`);
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async markAsUnread(workspaceSlug: string, notificationId: string): Promise<TNotification | undefined> {
    try {
      const { data } = await this.delete(
        `/api/workspaces/${workspaceSlug}/users/notifications/${notificationId}/read/`
      );
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async archive(workspaceSlug: string, notificationId: string): Promise<TNotification | undefined> {
    try {
      const { data } = await this.post(
        `/api/workspaces/${workspaceSlug}/users/notifications/${notificationId}/archive/`
      );
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async unarchive(workspaceSlug: string, notificationId: string): Promise<TNotification | undefined> {
    try {
      const { data } = await this.delete(
        `/api/workspaces/${workspaceSlug}/users/notifications/${notificationId}/archive/`
      );
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }

  async markAllAsRead(
    workspaceSlug: string,
    payload: TNotificationPaginatedInfoQueryParams
  ): Promise<TNotification | undefined> {
    try {
      const { data } = await this.post(`/api/workspaces/${workspaceSlug}/users/notifications/mark-all-read/`, payload);
      return data || undefined;
    } catch (error) {
      throw error;
    }
  }
}

export const workspaceNotificationService = new WorkspaceNotificationService();

export default workspaceNotificationService;
