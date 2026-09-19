/**
 * Copyright (c) 2023-present Plane Software, Inc. and contributors
 * SPDX-License-Identifier: AGPL-3.0-only
 * See the LICENSE file for details.
 */

import { useState, useEffect } from "react";
import Link from "@/lib/navigation/link";
import { useRouter } from "@/lib/navigation";
import { Controller, useForm } from "react-hook-form";
// pace imports
import { WEB_BASE_URL, ORGANIZATION_SIZE, RESTRICTED_URLS } from "@pace/constants";
import { Button } from "@makeplane/propel/components/button";
import { Input, InputGroup } from "@makeplane/propel/components/input";
import { Select, SelectContent, SelectItem, SelectList, SelectTrigger } from "@makeplane/propel/components/select";
import { InstanceWorkspaceService } from "@pace/services";
import type { IWorkspace } from "@pace/types";
import { validateSlug, validateWorkspaceName } from "@pace/utils";
// components
import { TOAST_TYPE, setToast } from "@/providers/toast";
// hooks
import { useWorkspace } from "@/hooks/store";
import { useTranslation } from "@pace/i18n";

const instanceWorkspaceService = new InstanceWorkspaceService();

// ORGANIZATION_SIZE entries are persisted verbatim as the `organization_size` field, so the stored value stays in English and only the visible label is translated. Any value without a mapping falls back to the raw constant rather than rendering a missing-key string.
const ORGANIZATION_SIZE_LABEL_KEYS: Record<string, string> = {
  "Just myself": "admin.forms.workspace.size_options.just_myself",
  "2-10": "admin.forms.workspace.size_options.range_2_10",
  "11-50": "admin.forms.workspace.size_options.range_11_50",
  "51-200": "admin.forms.workspace.size_options.range_51_200",
  "201-500": "admin.forms.workspace.size_options.range_201_500",
  "500+": "admin.forms.workspace.size_options.range_500_plus",
};

export function WorkspaceCreateForm() {
  const { t } = useTranslation();
  // router
  const router = useRouter();
  // states
  const [slugError, setSlugError] = useState(false);
  const [invalidSlug, setInvalidSlug] = useState(false);
  const [defaultValues, setDefaultValues] = useState<Partial<IWorkspace>>({
    name: "",
    slug: "",
    organization_size: "",
  });
  // store hooks
  const { createWorkspace } = useWorkspace();
  // form info
  const {
    handleSubmit,
    control,
    setValue,
    getValues,
    formState: { errors, isSubmitting, isValid },
  } = useForm<IWorkspace>({ defaultValues, mode: "onChange" });
  // derived values
  const [workspaceBaseURL, setWorkspaceBaseURL] = useState(() => encodeURI(WEB_BASE_URL || ""));

  useEffect(() => {
    if (!WEB_BASE_URL) {
      setWorkspaceBaseURL(encodeURI(window.location.origin + "/"));
    }
  }, []);

  const handleCreateWorkspace = async (formData: IWorkspace) => {
    await instanceWorkspaceService
      .slugCheck(formData.slug)
      .then(async (res) => {
        if (res.status === true && !RESTRICTED_URLS.includes(formData.slug)) {
          setSlugError(false);
          await createWorkspace(formData)
            .then(async () => {
              setToast({
                type: TOAST_TYPE.SUCCESS,
                title: t("admin.toast.success"),
                message: t("admin.workspace.created"),
              });
              router.push(`/workspace`);
            })
            .catch(() => {
              setToast({
                type: TOAST_TYPE.ERROR,
                title: t("admin.toast.error"),
                message: t("admin.workspace.create_failed"),
              });
            });
        } else setSlugError(true);
      })
      .catch(() => {
        setToast({
          type: TOAST_TYPE.ERROR,
          title: t("admin.toast.error"),
          message: t("admin.workspace.create_error"),
        });
      });
  };

  useEffect(
    () => () => {
      // when the component unmounts set the default values to whatever user typed in
      setDefaultValues(getValues());
    },
    [getValues, setDefaultValues]
  );

  return (
    <div className="space-y-8">
      <div className="grid-col grid w-full max-w-4xl grid-cols-1 items-start justify-between gap-x-10 gap-y-6 lg:grid-cols-2">
        <div className="flex flex-col gap-1">
          <h4 className="text-13 text-tertiary">{t("admin.forms.workspace.name_label")}</h4>
          <div className="flex flex-col gap-1">
            <Controller
              control={control}
              name="name"
              rules={{
                validate: (value) => validateWorkspaceName(value, true),
              }}
              render={({ field: { value, ref, onChange } }) => (
                <InputGroup size="lg">
                  <Input
                    size="lg"
                    id="workspaceName"
                    type="text"
                    value={value}
                    onChange={(e) => {
                      onChange(e.target.value);
                      setValue("name", e.target.value);
                      setValue("slug", e.target.value.toLocaleLowerCase().trim().replace(/ /g, "-"), {
                        shouldValidate: true,
                      });
                    }}
                    ref={ref}
                    aria-invalid={Boolean(errors.name)}
                    placeholder={t("admin.workspace.name_placeholder")}
                  />
                </InputGroup>
              )}
            />
            <span className="text-11 text-danger-primary">{errors?.name?.message}</span>
          </div>
        </div>
        <div className="flex flex-col gap-1">
          <h4 className="text-13 text-tertiary">{t("admin.forms.workspace.url_label")}</h4>
          <div className="flex w-full items-center gap-0.5 rounded-md border-[0.5px] border-subtle px-3">
            <span className="text-13 whitespace-nowrap text-secondary">{workspaceBaseURL}</span>
            <Controller
              control={control}
              name="slug"
              rules={{
                validate: (value) => validateSlug(value),
              }}
              render={({ field: { onChange, value, ref } }) => (
                <Input
                  id="workspaceUrl"
                  type="text"
                  size="lg"
                  value={value.toLocaleLowerCase().trim().replace(/ /g, "-")}
                  onChange={(e) => {
                    if (/^[a-zA-Z0-9_-]+$/.test(e.target.value)) setInvalidSlug(false);
                    else setInvalidSlug(true);
                    onChange(e.target.value.toLowerCase());
                  }}
                  ref={ref}
                  aria-invalid={Boolean(errors.slug)}
                  placeholder={t("admin.workspace.slug_placeholder")}
                />
              )}
            />
          </div>
          {slugError && <p className="text-13 text-danger-primary">{t("admin.forms.workspace.slug_taken")}</p>}
          {invalidSlug && <p className="text-13 text-danger-primary">{t("admin.forms.workspace.slug_invalid")}</p>}
          {errors.slug && <span className="text-11 text-danger-primary">{errors.slug.message}</span>}
        </div>
        <div className="flex flex-col gap-1">
          <h4 className="text-13 text-tertiary">{t("admin.forms.workspace.size_label")}</h4>
          <div className="w-full">
            <Controller
              name="organization_size"
              control={control}
              rules={{ required: t("admin.forms.workspace.required_field") }}
              render={({ field: { value, onChange } }) => (
                <Select value={value} onValueChange={onChange}>
                  <SelectTrigger
                    size="lg"
                    placeholder={
                      <span className="text-placeholder">{t("admin.forms.workspace.size_placeholder")}</span>
                    }
                  />
                  <SelectContent>
                    <SelectList>
                      {ORGANIZATION_SIZE.map((item) => {
                        const labelKey = ORGANIZATION_SIZE_LABEL_KEYS[item];
                        return <SelectItem key={item} value={item} label={labelKey ? t(labelKey) : item} size="lg" />;
                      })}
                    </SelectList>
                  </SelectContent>
                </Select>
              )}
            />
            {errors.organization_size && (
              <span className="text-13 text-danger-primary">{errors.organization_size.message}</span>
            )}
          </div>
        </div>
      </div>
      <div className="flex max-w-4xl items-center gap-4 py-1">
        <Button
          variant="primary"
          size="md"
          stretch="auto"
          onClick={handleSubmit(handleCreateWorkspace)}
          disabled={!isValid}
          loading={isSubmitting}
          label={isSubmitting ? t("admin.forms.workspace.creating") : t("admin.forms.workspace.create_submit")}
        />
        <Button
          variant="secondary"
          size="md"
          stretch="auto"
          nativeButton={false}
          render={<Link href="/workspace" />}
          label={t("admin.workspace.go_back")}
        />
      </div>
    </div>
  );
}
