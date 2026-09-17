/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useForm } from "react-hook-form";
import { ThoughtsOutline } from "@makeplane/propel/icons";
import { Button } from "@makeplane/propel/components/button";
import type { IFormattedInstanceConfiguration, TInstanceAIConfigurationKeys } from "@pace/types";
// components
import type { TControllerInputFormField } from "@/components/common/controller-input";
import { ControllerInput } from "@/components/common/controller-input";
import { TOAST_TYPE, setToast } from "@/providers/toast";
// hooks
import { useInstance } from "@/hooks/store";
import { ModelPicker } from "./model-picker";
import { ProviderPicker } from "./provider-picker";

type IInstanceAIForm = {
  config: IFormattedInstanceConfiguration;
};

type AIFormValues = Record<TInstanceAIConfigurationKeys, string>;

export function InstanceAIForm(props: IInstanceAIForm) {
  const { config } = props;
  // store
  const { updateInstanceConfigurations } = useInstance();
  // form data
  const {
    handleSubmit,
    control,
    setValue,
    getValues,
    formState: { errors, isSubmitting },
  } = useForm<AIFormValues>({
    // Every field the form submits has to be seeded here. What is submitted is the whole form object, so a field left out of this arrives as an empty string and overwrites whatever the instance had configured.
    defaultValues: {
      LLM_API_KEY: config["LLM_API_KEY"],
      LLM_MODEL: config["LLM_MODEL"],
      LLM_BASE_URL: config["LLM_BASE_URL"],
      LLM_PROVIDER: config["LLM_PROVIDER"],
    },
  });

  const aiFormFields: TControllerInputFormField<AIFormValues>[] = [
    {
      key: "LLM_BASE_URL",
      type: "text",
      label: "Base URL",
      description: (
        <>
          Where completions are asked for. Anything that answers an OpenAI-shaped <code>POST /chat/completions</code>{" "}
          works here — a gateway, a self-hosted server, or a provider&apos;s own endpoint. Leave it empty for OpenAI.
        </>
      ),
      placeholder: "https://api.openai.com/v1",
      error: Boolean(errors.LLM_BASE_URL),
      required: false,
    },
    {
      key: "LLM_API_KEY",
      type: "password",
      label: "API key",
      description: <>Sent as a bearer token to the base URL above.</>,
      placeholder: "sk-...",
      error: Boolean(errors.LLM_API_KEY),
      required: false,
    },
  ];

  const onSubmit = async (formData: AIFormValues) => {
    const payload: Partial<AIFormValues> = { ...formData };

    await updateInstanceConfigurations(payload)
      .then(() =>
        setToast({
          type: TOAST_TYPE.SUCCESS,
          title: "Success",
          message: "AI Settings updated successfully",
        })
      )
      .catch((err) => console.error(err));
  };

  return (
    <div className="space-y-8">
      <div className="space-y-3">
        <div>
          <div className="pb-1 text-18 font-medium text-primary">Language model</div>
          <div className="text-13 font-regular text-tertiary">
            The assistant speaks one protocol, so any endpoint that answers an OpenAI-shaped chat completion can serve
            it.
          </div>
        </div>
        <ProviderPicker
          control={control}
          onProviderChange={(baseURL) => {
            // Only the endpoint is filled in. The model is left as it is, because a model the operator typed is a deliberate choice and the backend already falls back to the provider's default when the field is empty.
            setValue("LLM_BASE_URL", baseURL ?? "", { shouldDirty: true });
          }}
        />
        <div className="grid-col grid w-full grid-cols-1 items-start justify-between gap-x-12 gap-y-8 lg:grid-cols-3">
          <ControllerInput
            control={control}
            type={aiFormFields[0].type}
            name={aiFormFields[0].key}
            label={aiFormFields[0].label}
            description={aiFormFields[0].description}
            placeholder={aiFormFields[0].placeholder}
            error={aiFormFields[0].error}
            required={aiFormFields[0].required}
          />
          <ModelPicker
            control={control}
            currentValues={() => ({
              baseURL: getValues("LLM_BASE_URL") ?? "",
              apiKey: getValues("LLM_API_KEY") ?? "",
              provider: getValues("LLM_PROVIDER") ?? "",
            })}
          />
          <ControllerInput
            control={control}
            type={aiFormFields[1].type}
            name={aiFormFields[1].key}
            label={aiFormFields[1].label}
            description={aiFormFields[1].description}
            placeholder={aiFormFields[1].placeholder}
            error={aiFormFields[1].error}
            required={aiFormFields[1].required}
          />
        </div>
      </div>

      <div className="flex flex-col items-start gap-4">
        <Button
          variant="primary"
          size="md"
          stretch="auto"
          onClick={handleSubmit(onSubmit)}
          loading={isSubmitting}
          label={isSubmitting ? "Saving" : "Save changes"}
        />

        <div className="relative inline-flex items-center gap-1.5 rounded-sm border border-accent-subtle bg-accent-subtle px-4 py-2 text-caption-sm-regular text-accent-secondary">
          <ThoughtsOutline className="size-4" />
          <div>Not listed above? Choose Custom and give it the base URL — anything OpenAI-shaped will do.</div>
        </div>
      </div>
    </div>
  );
}
