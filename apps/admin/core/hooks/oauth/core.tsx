/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { KeyOutline, MailOutline } from "@makeplane/propel/icons";
// pace internal packages
import { useTranslation } from "@pace/i18n";
// types
import type {
  TCoreInstanceAuthenticationModeKeys,
  TGetBaseAuthenticationModeProps,
  TInstanceAuthenticationModes,
} from "@pace/types";
// assets
import giteaLogo from "@/app/assets/logos/gitea-logo.svg?url";
import githubLightModeImage from "@/app/assets/logos/github-black.png?url";
import githubDarkModeImage from "@/app/assets/logos/github-white.png?url";
import gitlabLogo from "@/app/assets/logos/gitlab-logo.svg?url";
import googleLogo from "@/app/assets/logos/google-logo.svg?url";
// components
import { EmailCodesConfiguration } from "@/components/authentication/email-config-switch";
import { GiteaConfiguration } from "@/components/authentication/gitea-config";
import { GithubConfiguration } from "@/components/authentication/github-config";
import { GitlabConfiguration } from "@/components/authentication/gitlab-config";
import { GoogleConfiguration } from "@/components/authentication/google-config";
import { PasswordLoginConfiguration } from "@/components/authentication/password-config-switch";

// Authentication methods
// The map is built during the render pass of its caller so it can read translations itself, which makes it a hook: call it only from a component body or from another hook.
export const useCoreAuthenticationModesMap: (
  props: TGetBaseAuthenticationModeProps
) => Record<TCoreInstanceAuthenticationModeKeys, TInstanceAuthenticationModes> = ({
  disabled,
  updateConfig,
  resolvedTheme,
}) => {
  const { t } = useTranslation();

  return {
    "unique-codes": {
      key: "unique-codes",
      name: t("admin.common.unique_codes"),
      description: t("admin.oauth.unique_codes_description"),
      icon: <MailOutline className="h-6 w-6 p-0.5 text-tertiary" />,
      config: <EmailCodesConfiguration disabled={disabled} updateConfig={updateConfig} />,
      enabledConfigKey: "ENABLE_MAGIC_LINK_LOGIN",
    },
    "passwords-login": {
      key: "passwords-login",
      name: t("admin.common.passwords"),
      description: t("admin.oauth.passwords_description"),
      icon: <KeyOutline className="h-6 w-6 p-0.5 text-tertiary" />,
      config: <PasswordLoginConfiguration disabled={disabled} updateConfig={updateConfig} />,
      enabledConfigKey: "ENABLE_EMAIL_PASSWORD",
    },
    google: {
      key: "google",
      name: "Google",
      description: t("admin.oauth.description", { provider: "Google" }),
      icon: <img src={googleLogo} height={20} width={20} alt="Google Logo" />,
      config: <GoogleConfiguration disabled={disabled} updateConfig={updateConfig} />,
      enabledConfigKey: "IS_GOOGLE_ENABLED",
    },
    github: {
      key: "github",
      name: "GitHub",
      description: t("admin.oauth.description", { provider: "GitHub" }),
      icon: (
        <img
          src={resolvedTheme === "dark" ? githubDarkModeImage : githubLightModeImage}
          height={20}
          width={20}
          alt="GitHub Logo"
        />
      ),
      config: <GithubConfiguration disabled={disabled} updateConfig={updateConfig} />,
      enabledConfigKey: "IS_GITHUB_ENABLED",
    },
    gitlab: {
      key: "gitlab",
      name: "GitLab",
      description: t("admin.oauth.description", { provider: "GitLab" }),
      icon: <img src={gitlabLogo} height={20} width={20} alt="GitLab Logo" />,
      config: <GitlabConfiguration disabled={disabled} updateConfig={updateConfig} />,
      enabledConfigKey: "IS_GITLAB_ENABLED",
    },
    gitea: {
      key: "gitea",
      name: "Gitea",
      description: t("admin.oauth.description", { provider: "Gitea" }),
      icon: <img src={giteaLogo} height={20} width={20} alt="Gitea Logo" />,
      config: <GiteaConfiguration disabled={disabled} updateConfig={updateConfig} />,
      enabledConfigKey: "IS_GITEA_ENABLED",
    },
  };
};
