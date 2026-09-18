/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import Link from "@/app/hooks/link";
// pace packages
import type { TAdminAuthErrorInfo } from "@pace/constants";
import { SUPPORT_EMAIL, EAdminAuthErrorCodes } from "@pace/constants";
import { useTranslation } from "@pace/i18n";

export enum EErrorAlertType {
  BANNER_ALERT = "BANNER_ALERT",
  INLINE_FIRST_NAME = "INLINE_FIRST_NAME",
  INLINE_EMAIL = "INLINE_EMAIL",
  INLINE_PASSWORD = "INLINE_PASSWORD",
  INLINE_EMAIL_CODE = "INLINE_EMAIL_CODE",
}

// Translation of these strings happens at render time, not here: authErrorHandler is a plain function that callers invoke from effects and event handlers, where the useTranslation hook is not available. Messages are therefore returned as elements that translate themselves when React renders them, and titles are returned as i18n keys for the caller to pass through t().
const AUTH_ERROR_KEY_PREFIX = "admin.forms.auth_errors";

type TAuthErrorMessageProps = {
  i18nKey: string;
  values?: Record<string, unknown>;
};

function AuthErrorMessage({ i18nKey, values }: TAuthErrorMessageProps) {
  const { t } = useTranslation();
  return <>{t(i18nKey, values)}</>;
}

// Sentences that embed the sign-in link are stored as a single ICU message with a {link} argument so translators can place the link anywhere in the sentence. The argument is filled with a token that is split back out here and replaced by the real element.
const LINK_TOKEN = "__SIGN_IN_LINK__";

function AuthErrorSignInMessage({ i18nKey }: { i18nKey: string }) {
  const { t } = useTranslation();
  // A translation that drops the {link} argument yields a single segment, which would silently swallow the tail of the sentence. Append the link after the whole message in that case so no copy is lost.
  const segments = t(i18nKey, { link: LINK_TOKEN }).split(LINK_TOKEN);
  const before = segments[0] ?? "";
  const after = segments.length > 1 ? segments.slice(1).join(LINK_TOKEN) : "";
  return (
    <div>
      {before}
      <Link className="font-medium underline underline-offset-4 transition-all hover:font-bold" href={`/admin`}>
        {t(`${AUTH_ERROR_KEY_PREFIX}.sign_in_link`)}
      </Link>
      {after}
    </div>
  );
}

function AuthErrorDeactivatedMessage() {
  const { t } = useTranslation();
  const contact = SUPPORT_EMAIL ? SUPPORT_EMAIL : t(`${AUTH_ERROR_KEY_PREFIX}.administrator`);
  return <>{t(`${AUTH_ERROR_KEY_PREFIX}.admin_user_deactivated.message`, { contact })}</>;
}

const errorCodeMessages: {
  [key in EAdminAuthErrorCodes]: { titleKey: string; message: (email?: string) => React.ReactNode };
} = {
  // admin
  [EAdminAuthErrorCodes.ADMIN_ALREADY_EXIST]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.admin_already_exist.title`,
    message: () => <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.admin_already_exist.message`} />,
  },
  [EAdminAuthErrorCodes.REQUIRED_ADMIN_EMAIL_PASSWORD_FIRST_NAME]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.required_admin_email_password_first_name.title`,
    message: () => (
      <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.required_admin_email_password_first_name.message`} />
    ),
  },
  [EAdminAuthErrorCodes.INVALID_ADMIN_EMAIL]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.invalid_admin_email.title`,
    message: () => <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.invalid_admin_email.message`} />,
  },
  [EAdminAuthErrorCodes.INVALID_ADMIN_PASSWORD]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.invalid_admin_password.title`,
    message: () => <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.invalid_admin_password.message`} />,
  },
  [EAdminAuthErrorCodes.REQUIRED_ADMIN_EMAIL_PASSWORD]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.required_admin_email_password.title`,
    message: () => <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.required_admin_email_password.message`} />,
  },
  [EAdminAuthErrorCodes.ADMIN_AUTHENTICATION_FAILED]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.admin_authentication_failed.title`,
    message: () => <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.admin_authentication_failed.message`} />,
  },
  [EAdminAuthErrorCodes.ADMIN_USER_ALREADY_EXIST]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.admin_user_already_exist.title`,
    message: () => <AuthErrorSignInMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.admin_user_already_exist.message`} />,
  },
  [EAdminAuthErrorCodes.ADMIN_USER_DOES_NOT_EXIST]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.admin_user_does_not_exist.title`,
    message: () => <AuthErrorSignInMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.admin_user_does_not_exist.message`} />,
  },
  [EAdminAuthErrorCodes.ADMIN_USER_DEACTIVATED]: {
    titleKey: `${AUTH_ERROR_KEY_PREFIX}.admin_user_deactivated.title`,
    message: () => <AuthErrorDeactivatedMessage />,
  },
};

/**
 * Resolves an admin auth error code into banner data.
 *
 * `titleKey` is an i18n key, not display copy — run it through `t()` before rendering it. `message` is already a translated node and can be rendered as is.
 */
export const authErrorHandler = (errorCode: EAdminAuthErrorCodes, email?: string): TAdminAuthErrorInfo | undefined => {
  const bannerAlertErrorCodes = [
    EAdminAuthErrorCodes.ADMIN_ALREADY_EXIST,
    EAdminAuthErrorCodes.REQUIRED_ADMIN_EMAIL_PASSWORD_FIRST_NAME,
    EAdminAuthErrorCodes.INVALID_ADMIN_EMAIL,
    EAdminAuthErrorCodes.INVALID_ADMIN_PASSWORD,
    EAdminAuthErrorCodes.REQUIRED_ADMIN_EMAIL_PASSWORD,
    EAdminAuthErrorCodes.ADMIN_AUTHENTICATION_FAILED,
    EAdminAuthErrorCodes.ADMIN_USER_ALREADY_EXIST,
    EAdminAuthErrorCodes.ADMIN_USER_DOES_NOT_EXIST,
    EAdminAuthErrorCodes.ADMIN_USER_DEACTIVATED,
  ];

  if (bannerAlertErrorCodes.includes(errorCode))
    return {
      type: EErrorAlertType.BANNER_ALERT,
      code: errorCode,
      titleKey: errorCodeMessages[errorCode]?.titleKey || `${AUTH_ERROR_KEY_PREFIX}.default.title`,
      message: errorCodeMessages[errorCode]?.message(email) || (
        <AuthErrorMessage i18nKey={`${AUTH_ERROR_KEY_PREFIX}.default.message`} />
      ),
    };

  return undefined;
};
