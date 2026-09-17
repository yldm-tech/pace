/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useState } from "react";
import type { Control } from "react-hook-form";
import { Controller } from "react-hook-form";
import { Button } from "@makeplane/propel/components/button";
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
      if (response.models.length === 0) setProblem("The endpoint answered, but listed no models.");
    } catch (error) {
      // The server passes the endpoint's own status through, because 401 and 404 mean different things to whoever is filling this in.
      const detail = error as { error?: string; status?: number };
      const status = detail?.status;
      setProblem(
        status === 401 || status === 403
          ? "The endpoint rejected the API key."
          : status === 404
            ? "No model list at that URL. It usually needs the version segment, as in /v1."
            : (detail?.error ?? "The endpoint could not be reached.")
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
              Model
            </label>
            <Button
              variant="ghost"
              size="sm"
              stretch="auto"
              onClick={fetchModels}
              disabled={loading}
              label={loading ? "Asking…" : models ? "Refresh" : "Fetch models"}
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
              `${models.length} models. Start typing to filter, or leave it empty for the provider's default.`
            ) : (
              "Fetch the list from the endpoint above, or type a name. Empty uses the provider's default."
            )}
          </div>
        </div>
      )}
    />
  );
}
