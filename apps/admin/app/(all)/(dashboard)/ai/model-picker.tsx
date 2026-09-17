/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useState } from "react";
import type { Control } from "react-hook-form";
import { Controller } from "react-hook-form";
import { Button } from "@makeplane/propel/components/button";
import { useTranslation } from "@pace/i18n";
import type { TInstanceAIConfigurationKeys } from "@pace/types";
import { InstanceService } from "@pace/services";

const instanceService = new InstanceService();

type ModelPickerProps = {
  control: Control<Record<TInstanceAIConfigurationKeys, string>>;
  // Read at the moment of asking rather than passed in, because the endpoint and the key are usually being typed and the point is to ask with what is on screen now.
  currentValues: () => { baseURL: string; apiKey: string; provider: string };
};

// A model name used to be typed from memory, against a backend that then refused anything outside a hardcoded list. The list is gone, and the endpoint itself is the only thing that actually knows -- every OpenAI-shaped one answers GET /models -- so this asks it.
//
// It stays a text input rather than becoming a select. The list is a convenience, not a constraint: an endpoint may serve a model it does not advertise, and an operator who knows the name should not be blocked by a listing that omits it.
export function ModelPicker(props: ModelPickerProps) {
  const { control, currentValues } = props;
  const { t } = useTranslation();
  const [models, setModels] = useState<string[] | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [problem, setProblem] = useState<string | undefined>(undefined);

  const fetchModels = async () => {
    const { baseURL, apiKey, provider } = currentValues();
    setLoading(true);
    setProblem(undefined);
    try {
      const response = await instanceService.llmModels({
        base_url: baseURL || undefined,
        api_key: apiKey || undefined,
        provider: provider || undefined,
      });
      setModels(response.models);
      if (response.models.length === 0) setProblem(t("admin.ai.model.error_none"));
    } catch (error) {
      // The server passes the endpoint's own status through, because 401 and 404 mean different things to whoever is filling this in.
      const detail = error as { error?: string; status?: number };
      const status = detail?.status;
      setProblem(
        status === 401 || status === 403
          ? t("admin.ai.model.error_key")
          : status === 404
            ? t("admin.ai.model.error_not_found")
            : (detail?.error ?? t("admin.ai.model.error_unreachable"))
      );
      setModels(undefined);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Controller
      name="LLM_MODEL"
      control={control}
      render={({ field: { value, onChange } }) => (
        <div className="space-y-1.5">
          <div className="flex items-center justify-between gap-2">
            <label htmlFor="llm-model" className="text-13 font-medium text-primary">
              {t("admin.ai.model.label")}
            </label>
            <Button
              variant="ghost"
              size="sm"
              stretch="auto"
              onClick={fetchModels}
              disabled={loading}
              label={
                loading
                  ? t("admin.ai.model.fetching")
                  : models
                    ? t("admin.ai.model.refresh")
                    : t("admin.ai.model.fetch")
              }
            />
          </div>
          <input
            id="llm-model"
            type="text"
            value={value ?? ""}
            onChange={(event) => onChange(event.target.value)}
            placeholder="gpt-4o-mini"
            list={models ? "llm-model-options" : undefined}
            className="bg-surface focus:border-accent-primary w-full rounded-md border border-subtle px-3 py-2 text-13 text-primary outline-none"
          />
          {models && (
            <datalist id="llm-model-options">
              {models.map((model) => (
                <option key={model} value={model} />
              ))}
            </datalist>
          )}
          <div className="text-13 font-regular text-tertiary">
            {problem ? (
              <span className="text-danger">{problem}</span>
            ) : models ? (
              t("admin.ai.model.hint_loaded", { count: models.length })
            ) : (
              t("admin.ai.model.hint_empty")
            )}
          </div>
        </div>
      )}
    />
  );
}
