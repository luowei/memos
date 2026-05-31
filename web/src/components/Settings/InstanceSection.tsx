import { create } from "@bufbuild/protobuf";
import { useQueryClient } from "@tanstack/react-query";
import { isEqual } from "lodash-es";
import { useEffect, useState } from "react";
import { toast } from "react-hot-toast";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { identityProviderServiceClient } from "@/connect";
import { useInstance } from "@/contexts/InstanceContext";
import useDialog from "@/hooks/useDialog";
import { type GitHubSyncSetting, getAuthorizedJSONHeaders, gitHubSyncSettingKeys } from "@/hooks/useGitHubSyncSetting";
import { handleError } from "@/lib/error";
import { IdentityProvider } from "@/types/proto/api/v1/idp_service_pb";
import {
  InstanceSetting_GeneralSetting,
  InstanceSetting_GeneralSettingSchema,
  InstanceSetting_Key,
  InstanceSettingSchema,
} from "@/types/proto/api/v1/instance_service_pb";
import { useTranslate } from "@/utils/i18n";
import UpdateCustomizedProfileDialog from "../UpdateCustomizedProfileDialog";
import SettingGroup from "./SettingGroup";
import SettingRow from "./SettingRow";
import SettingSection from "./SettingSection";

const InstanceSection = () => {
  const t = useTranslate();
  const queryClient = useQueryClient();
  const customizeDialog = useDialog();
  const { generalSetting: originalSetting, profile, updateSetting, fetchSetting } = useInstance();
  const [instanceGeneralSetting, setInstanceGeneralSetting] = useState<InstanceSetting_GeneralSetting>(originalSetting);
  const [identityProviderList, setIdentityProviderList] = useState<IdentityProvider[]>([]);
  const [gitHubSyncSetting, setGitHubSyncSetting] = useState<GitHubSyncSetting>({
    hasToken: false,
    owner: "luowei",
    repo: "luowei_github_io_src",
    branch: "master",
    apiBaseUrl: "https://api.github.com",
    hideMemoAction: true,
    secondBrainBaseUrl: "https://wiki.markdev.work",
    hasSecondBrainSharedSecret: false,
  });
  const [gitHubSyncToken, setGitHubSyncToken] = useState("");
  const [clearGitHubSyncToken, setClearGitHubSyncToken] = useState(false);
  const [secondBrainSharedSecret, setSecondBrainSharedSecret] = useState("");
  const [clearSecondBrainSharedSecret, setClearSecondBrainSharedSecret] = useState(false);
  const [isSavingGitHubSyncSetting, setIsSavingGitHubSyncSetting] = useState(false);

  useEffect(() => {
    setInstanceGeneralSetting((prev) =>
      create(InstanceSetting_GeneralSettingSchema, {
        ...prev,
        customProfile: originalSetting.customProfile,
      }),
    );
  }, [originalSetting.customProfile]);

  const fetchIdentityProviderList = async () => {
    const { identityProviders } = await identityProviderServiceClient.listIdentityProviders({});
    setIdentityProviderList(identityProviders);
  };

  useEffect(() => {
    fetchIdentityProviderList();
  }, []);

  const fetchGitHubSyncSetting = async () => {
    const response = await fetch("/api/v1/integrations/github-sync", {
      credentials: "include",
      headers: await getAuthorizedJSONHeaders(),
    });
    if (!response.ok) {
      throw new Error(`Failed to fetch GitHub sync setting with status ${response.status}`);
    }
    const data = (await response.json()) as GitHubSyncSetting;
    setGitHubSyncSetting(data);
    setGitHubSyncToken("");
    setClearGitHubSyncToken(false);
    setSecondBrainSharedSecret("");
    setClearSecondBrainSharedSecret(false);
  };

  useEffect(() => {
    fetchGitHubSyncSetting().catch((error) => {
      console.error("Failed to fetch GitHub sync setting:", error);
    });
  }, []);

  const updatePartialSetting = (partial: Partial<InstanceSetting_GeneralSetting>) => {
    setInstanceGeneralSetting(
      create(InstanceSetting_GeneralSettingSchema, {
        ...instanceGeneralSetting,
        ...partial,
      }),
    );
  };

  const handleSaveGeneralSetting = async () => {
    try {
      await updateSetting(
        create(InstanceSettingSchema, {
          name: `instance/settings/${InstanceSetting_Key[InstanceSetting_Key.GENERAL]}`,
          value: {
            case: "generalSetting",
            value: instanceGeneralSetting,
          },
        }),
      );
      await fetchSetting(InstanceSetting_Key.GENERAL);
    } catch (error: unknown) {
      await handleError(error, toast.error, {
        context: "Update general settings",
      });
      return;
    }
    toast.success(t("message.update-succeed"));
  };

  const handleSaveGitHubSyncSetting = async () => {
    setIsSavingGitHubSyncSetting(true);
    try {
      const response = await fetch("/api/v1/integrations/github-sync", {
        method: "PUT",
        credentials: "include",
        headers: await getAuthorizedJSONHeaders(),
        body: JSON.stringify({
          token: gitHubSyncToken.trim(),
          owner: gitHubSyncSetting.owner.trim(),
          repo: gitHubSyncSetting.repo.trim(),
          branch: gitHubSyncSetting.branch.trim(),
          apiBaseUrl: gitHubSyncSetting.apiBaseUrl.trim(),
          clearToken: clearGitHubSyncToken,
          hideMemoAction: gitHubSyncSetting.hideMemoAction,
          secondBrainBaseUrl: gitHubSyncSetting.secondBrainBaseUrl.trim(),
          secondBrainSharedSecret: secondBrainSharedSecret.trim(),
          clearSecondBrainSharedSecret,
        }),
      });
      if (!response.ok) {
        const error = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(error?.message || `Failed to save GitHub sync setting with status ${response.status}`);
      }
      const data = (await response.json()) as GitHubSyncSetting;
      setGitHubSyncSetting(data);
      setGitHubSyncToken("");
      setClearGitHubSyncToken(false);
      setSecondBrainSharedSecret("");
      setClearSecondBrainSharedSecret(false);
      queryClient.invalidateQueries({ queryKey: gitHubSyncSettingKeys.detail() });
      toast.success(t("message.update-succeed"));
    } catch (error: unknown) {
      await handleError(error, toast.error, {
        context: "Update GitHub sync settings",
      });
    } finally {
      setIsSavingGitHubSyncSetting(false);
    }
  };

  return (
    <SettingSection title={t("setting.system.label")}>
      <SettingGroup title={t("common.basic")}>
        <SettingRow label={t("setting.system.server-name")} description={instanceGeneralSetting.customProfile?.title || "Memos"}>
          <Button variant="outline" onClick={customizeDialog.open}>
            {t("common.edit")}
          </Button>
        </SettingRow>
      </SettingGroup>

      <SettingGroup title={t("setting.system.title")} showSeparator>
        <SettingRow label={t("setting.system.additional-style")} vertical>
          <Textarea
            className="font-mono w-full"
            rows={3}
            placeholder={t("setting.system.additional-style-placeholder")}
            value={instanceGeneralSetting.additionalStyle}
            onChange={(event) => updatePartialSetting({ additionalStyle: event.target.value })}
          />
        </SettingRow>

        <SettingRow label={t("setting.system.additional-script")} vertical>
          <Textarea
            className="font-mono w-full"
            rows={3}
            placeholder={t("setting.system.additional-script-placeholder")}
            value={instanceGeneralSetting.additionalScript}
            onChange={(event) => updatePartialSetting({ additionalScript: event.target.value })}
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup showSeparator>
        <SettingRow label={t("setting.instance.disallow-user-registration")}>
          <Switch
            disabled={profile.demo}
            checked={instanceGeneralSetting.disallowUserRegistration}
            onCheckedChange={(checked) => updatePartialSetting({ disallowUserRegistration: checked })}
          />
        </SettingRow>

        <SettingRow label={t("setting.instance.disallow-password-auth")}>
          <Switch
            disabled={profile.demo || (identityProviderList.length === 0 && !instanceGeneralSetting.disallowPasswordAuth)}
            checked={instanceGeneralSetting.disallowPasswordAuth}
            onCheckedChange={(checked) => updatePartialSetting({ disallowPasswordAuth: checked })}
          />
        </SettingRow>

        <SettingRow label={t("setting.instance.disallow-change-username")}>
          <Switch
            checked={instanceGeneralSetting.disallowChangeUsername}
            onCheckedChange={(checked) => updatePartialSetting({ disallowChangeUsername: checked })}
          />
        </SettingRow>

        <SettingRow label={t("setting.instance.disallow-change-nickname")}>
          <Switch
            checked={instanceGeneralSetting.disallowChangeNickname}
            onCheckedChange={(checked) => updatePartialSetting({ disallowChangeNickname: checked })}
          />
        </SettingRow>

        <SettingRow label={t("setting.instance.week-start-day")}>
          <Select
            value={instanceGeneralSetting.weekStartDayOffset.toString()}
            onValueChange={(value) => {
              updatePartialSetting({ weekStartDayOffset: parseInt(value) || 0 });
            }}
          >
            <SelectTrigger className="min-w-fit">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="-1">{t("setting.instance.saturday")}</SelectItem>
              <SelectItem value="0">{t("setting.instance.sunday")}</SelectItem>
              <SelectItem value="1">{t("setting.instance.monday")}</SelectItem>
            </SelectContent>
          </Select>
        </SettingRow>
      </SettingGroup>

      <SettingGroup title={t("setting.system.github-sync.title")} description={t("setting.system.github-sync.description")} showSeparator>
        <SettingRow
          label={t("setting.system.github-sync.hide-memo-action")}
          description={t("setting.system.github-sync.hide-memo-action-description")}
        >
          <Switch
            checked={gitHubSyncSetting.hideMemoAction}
            onCheckedChange={(checked) => setGitHubSyncSetting((prev) => ({ ...prev, hideMemoAction: checked }))}
          />
        </SettingRow>

        <SettingRow label={t("setting.system.github-sync.owner")} vertical>
          <Input
            value={gitHubSyncSetting.owner}
            onChange={(event) => setGitHubSyncSetting((prev) => ({ ...prev, owner: event.target.value }))}
            className="w-full"
          />
        </SettingRow>

        <SettingRow label={t("setting.system.github-sync.repo")} vertical>
          <Input
            value={gitHubSyncSetting.repo}
            onChange={(event) => setGitHubSyncSetting((prev) => ({ ...prev, repo: event.target.value }))}
            className="w-full"
          />
        </SettingRow>

        <SettingRow label={t("setting.system.github-sync.branch")} vertical>
          <Input
            value={gitHubSyncSetting.branch}
            onChange={(event) => setGitHubSyncSetting((prev) => ({ ...prev, branch: event.target.value }))}
            className="w-full"
          />
        </SettingRow>

        <SettingRow label={t("setting.system.github-sync.api-base-url")} vertical>
          <Input
            value={gitHubSyncSetting.apiBaseUrl}
            onChange={(event) => setGitHubSyncSetting((prev) => ({ ...prev, apiBaseUrl: event.target.value }))}
            className="w-full"
          />
        </SettingRow>

        <SettingRow
          label={t("setting.system.github-sync.token")}
          description={
            gitHubSyncSetting.hasToken
              ? gitHubSyncSetting.tokenHint || t("setting.system.github-sync.token-configured")
              : t("setting.system.github-sync.token-not-configured")
          }
          vertical
        >
          <div className="w-full flex flex-col gap-2">
            <Input
              type="password"
              value={gitHubSyncToken}
              onChange={(event) => {
                setGitHubSyncToken(event.target.value);
                if (event.target.value) {
                  setClearGitHubSyncToken(false);
                }
              }}
              placeholder={t("setting.system.github-sync.token-placeholder")}
              className="w-full"
            />
            {gitHubSyncSetting.hasToken && (
              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                <Switch checked={clearGitHubSyncToken} onCheckedChange={setClearGitHubSyncToken} />
                <span>{t("setting.system.github-sync.clear-token")}</span>
              </label>
            )}
          </div>
        </SettingRow>

        <SettingRow label={t("setting.system.github-sync.second-brain-base-url")} vertical>
          <Input
            value={gitHubSyncSetting.secondBrainBaseUrl}
            onChange={(event) => setGitHubSyncSetting((prev) => ({ ...prev, secondBrainBaseUrl: event.target.value }))}
            className="w-full"
          />
        </SettingRow>

        <SettingRow
          label={t("setting.system.github-sync.second-brain-shared-secret")}
          description={
            gitHubSyncSetting.hasSecondBrainSharedSecret
              ? gitHubSyncSetting.secondBrainSharedSecretHint || t("setting.system.github-sync.second-brain-secret-configured")
              : t("setting.system.github-sync.second-brain-secret-not-configured")
          }
          vertical
        >
          <div className="w-full flex flex-col gap-2">
            <Input
              type="password"
              value={secondBrainSharedSecret}
              onChange={(event) => {
                setSecondBrainSharedSecret(event.target.value);
                if (event.target.value) {
                  setClearSecondBrainSharedSecret(false);
                }
              }}
              placeholder={t("setting.system.github-sync.second-brain-secret-placeholder")}
              className="w-full"
            />
            {gitHubSyncSetting.hasSecondBrainSharedSecret && (
              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                <Switch checked={clearSecondBrainSharedSecret} onCheckedChange={setClearSecondBrainSharedSecret} />
                <span>{t("setting.system.github-sync.clear-second-brain-secret")}</span>
              </label>
            )}
          </div>
        </SettingRow>

        <div className="w-full flex justify-end">
          <Button type="button" onClick={handleSaveGitHubSyncSetting} disabled={isSavingGitHubSyncSetting}>
            {t("common.save")}
          </Button>
        </div>
      </SettingGroup>

      <div className="w-full flex justify-end">
        <Button disabled={isEqual(instanceGeneralSetting, originalSetting)} onClick={handleSaveGeneralSetting}>
          {t("common.save")}
        </Button>
      </div>

      <UpdateCustomizedProfileDialog
        open={customizeDialog.isOpen}
        onOpenChange={customizeDialog.setOpen}
        onSuccess={() => {
          toast.success(t("message.update-succeed"));
        }}
      />
    </SettingSection>
  );
};

export default InstanceSection;
