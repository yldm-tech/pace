/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import type { Control } from "react-hook-form";
import { Controller } from "react-hook-form";
import { useTranslation } from "@pace/i18n";
import type { TInstanceAIConfigurationKeys } from "@pace/types";
// Brand marks come from @yldm-tech/ai-logo-static-svg, which is svg files and nothing else -- no dependencies and no peers.
//
// Its React sibling, @yldm-tech/ai-logo, was the obvious choice and is not usable here: a brand icon there is a compound component whose barrel pulls in IconAvatar, IconAvatar imports antd-style, and antd-style takes antd as a non-optional peer. Importing one mark would have installed antd and antd-style -- 616 packages, for four svgs.
import anthropicIcon from "@yldm-tech/ai-logo-static-svg/icons/anthropic.svg?url";
import everyapiIcon from "@yldm-tech/ai-logo-static-svg/icons/everyapi-color.svg?url";
import geminiIcon from "@yldm-tech/ai-logo-static-svg/icons/gemini-color.svg?url";
import openaiIcon from "@yldm-tech/ai-logo-static-svg/icons/openai.svg?url";

// The providers the backend knows a default model and endpoint for. Naming one is a convenience: it fills in the model and the base URL so neither has to be typed.
//
// It is not a closed list. Anything speaking an OpenAI-shaped chat completion works, which is what the custom option is -- it only asks that the base URL and the model be named, because there is no default to infer them from.
export const KNOWN_PROVIDERS = [
  { value: "everyapi", label: "EveryAPI", icon: everyapiIcon, baseURL: "https://api.everyapi.ai/v1" },
  { value: "openai", label: "OpenAI", icon: openaiIcon, baseURL: "https://api.openai.com/v1" },
  { value: "anthropic", label: "Anthropic", icon: anthropicIcon, baseURL: "https://api.anthropic.com/v1" },
  {
    value: "gemini",
    label: "Gemini",
    icon: geminiIcon,
    baseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
  },
] as const;

type ProviderPickerProps = {
  control: Control<Record<TInstanceAIConfigurationKeys, string>>;
  // Called when a known provider is chosen, so the form can fill in the endpoint it answers on. Choosing the custom option passes nothing, because there is no endpoint to infer.
  onProviderChange: (baseURL: string | undefined) => void;
};

export function ProviderPicker(props: ProviderPickerProps) {
  const { control, onProviderChange } = props;
  const { t } = useTranslation();

  return (
    <Controller
      name="LLM_PROVIDER"
      control={control}
      render={({ field: { value, onChange } }) => {
        // Anything that is not one of the four is the custom case, including an empty value on a fresh install.
        const isKnown = KNOWN_PROVIDERS.some((provider) => provider.value === value);

        return (
          <div className="space-y-2">
            <div className="text-13 font-medium text-primary">{t("admin.ai.provider.label")}</div>
            <div className="flex flex-wrap gap-2">
              {KNOWN_PROVIDERS.map((provider) => (
                <button
                  key={provider.value}
                  type="button"
                  aria-pressed={value === provider.value}
                  onClick={() => {
                    onChange(provider.value);
                    onProviderChange(provider.baseURL);
                  }}
                  className={`flex items-center gap-2 rounded-md border px-3 py-2 text-13 transition-colors ${
                    value === provider.value
                      ? "border-accent-primary bg-accent-primary/5 text-primary"
                      : "border-subtle text-secondary hover:border-strong"
                  }`}
                >
                  <img src={provider.icon} alt="" aria-hidden="true" className="size-4" />
                  {provider.label}
                </button>
              ))}
              <button
                type="button"
                aria-pressed={!isKnown}
                onClick={() => {
                  // Cleared rather than set to a placeholder, so the field below shows what this installation actually sends.
                  onChange("");
                  onProviderChange(undefined);
                }}
                className={`flex items-center gap-2 rounded-md border px-3 py-2 text-13 transition-colors ${
                  !isKnown
                    ? "border-accent-primary bg-accent-primary/5 text-primary"
                    : "border-subtle text-secondary hover:border-strong"
                }`}
              >
                {t("admin.ai.provider.custom")}
              </button>
            </div>
            <div className="text-13 font-regular text-tertiary">{t("admin.ai.provider.description")}</div>
          </div>
        );
      }}
    />
  );
}
