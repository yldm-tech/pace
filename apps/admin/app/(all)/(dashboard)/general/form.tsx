/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { observer } from "mobx-react";
import { Controller, useForm } from "react-hook-form";
import { UsageOutline } from "@makeplane/propel/icons";
// pace imports
import { Button } from "@makeplane/propel/components/button";
import { Input } from "@makeplane/propel/components/input";
import { Switch } from "@makeplane/propel/components/switch";
import { DOCS_URL, externalLink } from "@pace/constants";
import type { IInstance, IInstanceAdmin } from "@pace/types";
// components
import { ControllerInput } from "@/components/common/controller-input";
import { TOAST_TYPE, setToast } from "@/providers/toast";
// hooks
import { useTranslation } from "@pace/i18n";
import { useInstance } from "@/hooks/store";

const telemetryPolicyLink = externalLink(DOCS_URL, "/self-hosting/telemetry");

export interface IGeneralConfigurationForm {
  instance: IInstance;
  instanceAdmins: IInstanceAdmin[];
}

export const GeneralConfigurationForm = observer(function GeneralConfigurationForm(props: IGeneralConfigurationForm) {
  const { instance, instanceAdmins } = props;
  // hooks
  const { updateInstanceInfo } = useInstance();
  const { t } = useTranslation();

  // form data
  const {
    handleSubmit,
    control,
    formState: { errors, isSubmitting },
  } = useForm<Partial<IInstance>>({
    defaultValues: {
      instance_name: instance?.instance_name,
      is_telemetry_enabled: instance?.is_telemetry_enabled,
    },
  });

  const onSubmit = async (formData: Partial<IInstance>) => {
    const payload: Partial<IInstance> = { ...formData };

    await updateInstanceInfo(payload)
      .then(() =>
        setToast({
          type: TOAST_TYPE.SUCCESS,
          title: t("admin.toast.success"),
          message: t("admin.general.saved"),
        })
      )
      .catch((err) => console.error(err));
  };

  return (
    <div className="space-y-8">
      <div className="space-y-4">
        <div className="text-16 font-medium text-primary">{t("admin.general.heading")}</div>
        <div className="grid-col grid w-full grid-cols-1 items-center justify-between gap-8 md:grid-cols-2 lg:grid-cols-3">
          <ControllerInput
            key="instance_name"
            name="instance_name"
            control={control}
            type="text"
            label={t("admin.general.name.label")}
            placeholder={t("admin.general.name.placeholder")}
            error={Boolean(errors.instance_name)}
            required
          />

          <div className="flex flex-col gap-1">
            <h4 className="text-13 text-tertiary">{t("admin.general.email.label")}</h4>
            <div className="w-full">
              <Input
                id="email"
                name="email"
                type="email"
                size="lg"
                value={instanceAdmins[0]?.user_detail?.email ?? ""}
                placeholder={t("admin.general.email.placeholder")}
                autoComplete="on"
                disabled
              />
            </div>
          </div>

          <div className="flex flex-col gap-1">
            <h4 className="text-13 text-tertiary">{t("admin.general.instance_id")}</h4>
            <div className="w-full">
              <Input id="instance_id" name="instance_id" type="text" size="lg" value={instance.instance_id} disabled />
            </div>
          </div>
        </div>
      </div>

      <div className="space-y-6">
        <div className="border-b border-subtle pb-1.5 text-16 font-medium text-primary">
          {t("admin.general.telemetry.label")}
        </div>
        <div className="flex items-center gap-14">
          <div className="flex grow items-center gap-4">
            <div className="shrink-0">
              <div className="flex size-11 items-center justify-center rounded-lg bg-layer-1">
                <UsageOutline className="size-5 text-tertiary" />
              </div>
            </div>
            <div className="grow">
              <div className="text-13 leading-5 font-medium text-primary">
                {t("admin.general.telemetry.description")}
              </div>
              <div className="text-11 leading-5 font-regular text-tertiary">
                {/* The sentence carries the actual disclosure -- no PII, anonymous, used for feature work -- so it stays either way. What goes with the link is the clause that cites the policy document: naming a Telemetry Policy the reader cannot open claims a document this installation may not publish. Upstream this page linked developers.plane.so, and the docs site is the closest equivalent a fork configures. */}
                {telemetryPolicyLink ? (
                  <>
                    No PII is collected. This anonymized data is used to understand how you use Plane and build new
                    features in line with{" "}
                    <a
                      href={telemetryPolicyLink}
                      target="_blank"
                      className="text-accent-primary hover:underline"
                      rel="noreferrer"
                    >
                      our Telemetry Policy.
                    </a>
                  </>
                ) : (
                  "No PII is collected. This anonymized data is used to understand how you use Plane and build new features."
                )}
              </div>
            </div>
          </div>
          <div className={`shrink-0 ${isSubmitting && "opacity-70"}`}>
            <Controller
              control={control}
              name="is_telemetry_enabled"
              render={({ field: { value, onChange } }) => (
                <Switch checked={value ?? false} onCheckedChange={onChange} size="sm" disabled={isSubmitting} />
              )}
            />
          </div>
        </div>
      </div>

      <div>
        <Button
          variant="primary"
          size="md"
          stretch="auto"
          onClick={() => {
            void handleSubmit(onSubmit)();
          }}
          loading={isSubmitting}
          label={isSubmitting ? t("admin.actions.saving") : t("admin.actions.save")}
        />
      </div>
    </div>
  );
});
