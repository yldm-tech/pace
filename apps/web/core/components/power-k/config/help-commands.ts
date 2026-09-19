/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { ChatOutline, DocumentationOutline, Github, RocketOutline } from "@pace/propel/icons";
// pace imports
import { DOCS_URL, FORUM_URL } from "@pace/constants";
// components
import type { TPowerKCommandConfig } from "@/components/power-k/core/types";
// hooks
import { usePowerK } from "@/hooks/store/use-power-k";

/**
 * Help commands - Help related commands
 */
export const usePowerKHelpCommands = (): TPowerKCommandConfig[] => {
  // store
  const { toggleShortcutsListModal } = usePowerK();

  return [
    {
      id: "open_keyboard_shortcuts",
      type: "action",
      group: "help",
      i18n_title: "power_k.help_actions.open_keyboard_shortcuts",
      icon: RocketOutline,
      modifierShortcut: "cmd+/",
      action: () => toggleShortcutsListModal(true),
      isEnabled: () => true,
      isVisible: () => true,
      closeOnSelect: true,
    },
    {
      id: "open_plane_documentation",
      type: "action",
      group: "help",
      i18n_title: "power_k.help_actions.open_plane_documentation",
      icon: DocumentationOutline,
      action: () => {
        window.open(DOCS_URL, "_blank", "noopener,noreferrer");
      },
      isEnabled: () => true,
      isVisible: () => !!DOCS_URL,
      closeOnSelect: true,
    },
    {
      id: "join_forum",
      type: "action",
      group: "help",
      i18n_title: "power_k.help_actions.join_forum",
      icon: ChatOutline,
      action: () => {
        window.open(FORUM_URL, "_blank", "noopener,noreferrer");
      },
      isEnabled: () => true,
      isVisible: () => !!FORUM_URL,
      closeOnSelect: true,
    },
    {
      id: "report_bug",
      type: "action",
      group: "help",
      i18n_title: "power_k.help_actions.report_bug",
      icon: Github,
      action: () => {
        window.open("https://github.com/makeplane/plane/issues/new/choose", "_blank", "noopener,noreferrer");
      },
      isEnabled: () => true,
      isVisible: () => true,
      closeOnSelect: true,
    },
  ];
};
