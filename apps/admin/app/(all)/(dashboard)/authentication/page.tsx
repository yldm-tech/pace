/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { observer } from "mobx-react";
import { useTheme } from "next-themes";
import useSWR from "swr";
// pace internal packages
import { Switch } from "@makeplane/propel/components/switch";
import type { TInstanceConfigurationKeys, TInstanceAuthenticationModes } from "@pace/types";
import { cn, resolveGeneralTheme } from "@pace/utils";
// components
import { PageWrapper } from "@/components/common/page-wrapper";
import { AuthenticationMethodCard } from "@/components/authentication/authentication-method-card";
import { Skeleton } from "@/components/common/skeleton";
import { setPromiseToast, setToast, TOAST_TYPE } from "@/providers/toast";
// helpers
import { canDisableAuthMethod } from "@/helpers/authentication";
// hooks
import { useAuthenticationModes } from "@/hooks/oauth";
import { useInstance } from "@/hooks/store";
// types
import type { Route } from "./+types/page";
import { useTranslation } from "@pace/i18n";

const InstanceAuthenticationPage = observer(function InstanceAuthenticationPage(_props: Route.ComponentProps) {
  const { t } = useTranslation();
  // theme
  const { resolvedTheme: resolvedThemeAdmin } = useTheme();
  const resolvedTheme = resolveGeneralTheme(resolvedThemeAdmin);
  // Ref to store authentication modes for validation (avoids circular dependency)
  const authenticationModesRef = useRef<TInstanceAuthenticationModes[]>([]);
  // state
  const [isSubmitting, setIsSubmitting] = useState<boolean>(false);
  // store hooks
  const { fetchInstanceConfigurations, formattedConfig, updateInstanceConfigurations } = useInstance();
  // derived values
  const enableSignUpConfig = formattedConfig?.ENABLE_SIGNUP ?? "";

  useSWR("INSTANCE_CONFIGURATIONS", () => fetchInstanceConfigurations());

  // Create updateConfig with validation - uses authenticationModesRef for current modes
  const updateConfig = useCallback(
    (key: TInstanceConfigurationKeys, value: string): void => {
      // Check if trying to disable (value === "0")
      if (value === "0") {
        // Check if this key is an authentication method key
        const currentAuthModes = authenticationModesRef.current;
        const isAuthMethodKey = currentAuthModes.some((method) => method.enabledConfigKey === key);

        // Only validate if this is an authentication method key
        if (isAuthMethodKey) {
          const canDisable = canDisableAuthMethod(key, currentAuthModes, formattedConfig);

          if (!canDisable) {
            setToast({
              type: TOAST_TYPE.ERROR,
              title: t("admin.auth.cannot_disable"),
              message: t("admin.page.authentication.cannot_disable_message"),
            });
            return;
          }
        }
      }

      // Proceed with the update
      setIsSubmitting(true);

      const payload = {
        [key]: value,
      };

      const updateConfigPromise = updateInstanceConfigurations(payload);

      setPromiseToast(updateConfigPromise, {
        loading: t("admin.auth.saving"),
        success: {
          title: t("admin.toast.success"),
          message: () => t("admin.auth.saved"),
        },
        error: {
          title: t("admin.toast.error"),
          message: () => t("admin.auth.save_failed"),
        },
      });

      void updateConfigPromise
        .then(() => {
          setIsSubmitting(false);
          return undefined;
        })
        .catch((err) => {
          console.error(err);
          setIsSubmitting(false);
        });
    },
    [formattedConfig, updateInstanceConfigurations]
  );

  // Get authentication modes - this will use updateConfig which includes validation
  const authenticationModes = useAuthenticationModes({
    disabled: isSubmitting,
    updateConfig,
    resolvedTheme,
  });

  // Update ref with latest authentication modes (updateConfig reads it only from event handlers)
  useEffect(() => {
    authenticationModesRef.current = authenticationModes;
  }, [authenticationModes]);

  return (
    <PageWrapper
      header={{
        title: t("admin.auth.heading"),
        description: t("admin.page.authentication.description"),
      }}
    >
      {formattedConfig ? (
        <div className="space-y-3">
          <div className={cn("flex w-full items-center gap-14 rounded-sm")}>
            <div className="flex grow items-center gap-4">
              <div className="grow">
                <div className="pb-1 text-16 font-medium">{t("admin.auth.allow_signup")}</div>
                <div className={cn("text-11 leading-5 font-regular text-tertiary")}>
                  {t("admin.page.authentication.allow_signup_description")}
                </div>
              </div>
            </div>
            <div className={`shrink-0 pr-4 ${isSubmitting && "opacity-70"}`}>
              <div className="flex items-center gap-4">
                <Switch
                  checked={Boolean(parseInt(enableSignUpConfig))}
                  onCheckedChange={() => {
                    if (Boolean(parseInt(enableSignUpConfig)) === true) {
                      updateConfig("ENABLE_SIGNUP", "0");
                    } else {
                      updateConfig("ENABLE_SIGNUP", "1");
                    }
                  }}
                  size="sm"
                  disabled={isSubmitting}
                />
              </div>
            </div>
          </div>
          <div className="text-lg pt-6 font-medium">{t("admin.auth.available_modes")}</div>
          {authenticationModes.map((method) => (
            <AuthenticationMethodCard
              key={method.key}
              name={method.name}
              description={method.description}
              icon={method.icon}
              config={method.config}
              disabled={isSubmitting}
              unavailable={method.unavailable}
            />
          ))}
        </div>
      ) : (
        <Skeleton className="space-y-10">
          <Skeleton.Item height="50px" width="75%" />
          <Skeleton.Item height="50px" width="75%" />
          <Skeleton.Item height="50px" width="40%" />
          <Skeleton.Item height="50px" width="40%" />
          <Skeleton.Item height="50px" width="20%" />
        </Skeleton>
      )}
    </PageWrapper>
  );
});

export const meta: Route.MetaFunction = () => [{ title: "Authentication Settings - Pace Admin" }];

export default InstanceAuthenticationPage;
