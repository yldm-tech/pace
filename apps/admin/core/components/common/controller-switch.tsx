/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import type { Control, FieldPath, FieldValues } from "react-hook-form";
import { Controller } from "react-hook-form";
// pace internal packages
import { Switch } from "@makeplane/propel/components/switch";
import { useTranslation } from "@pace/i18n";

type Props<T extends FieldValues = FieldValues> = {
  control: Control<T>;
  field: TControllerSwitchFormField<T>;
};

export type TControllerSwitchFormField<T extends FieldValues = FieldValues> = {
  name: FieldPath<T>;
  // Brand name of the OAuth provider, interpolated into the translated caption.
  provider: string;
};

export function ControllerSwitch<T extends FieldValues>(props: Props<T>) {
  const {
    control,
    field: { name, provider },
  } = props;
  // i18n
  const { t } = useTranslation();

  return (
    <div className="flex items-center justify-between gap-1">
      <h4 className="text-sm text-custom-text-300">{t("admin.oauth.refresh_attributes", { provider })}</h4>
      <div className="relative">
        <Controller
          control={control}
          name={name as FieldPath<T>}
          render={({ field: { value, onChange } }) => {
            const parsedValue = Number.parseInt(typeof value === "string" ? value : String(value ?? "0"), 10);
            const isOn = !Number.isNaN(parsedValue) && parsedValue !== 0;
            return <Switch checked={isOn} onCheckedChange={() => onChange(isOn ? "0" : "1")} size="sm" />;
          }}
        />
      </div>
    </div>
  );
}
