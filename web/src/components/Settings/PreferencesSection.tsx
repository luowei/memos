import { create } from "@bufbuild/protobuf";
import { useState } from "react";
import toast from "react-hot-toast";
import { getAccessToken, hasStoredToken, isTokenExpired } from "@/auth-state";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { refreshAccessToken } from "@/connect";
import { useAuth } from "@/contexts/AuthContext";
import { useUpdateUserGeneralSetting } from "@/hooks/useUserQueries";
import { handleError } from "@/lib/error";
import { Visibility } from "@/types/proto/api/v1/memo_service_pb";
import { UserSetting_GeneralSetting, UserSetting_GeneralSettingSchema } from "@/types/proto/api/v1/user_service_pb";
import { loadLocale, useTranslate } from "@/utils/i18n";
import { convertVisibilityFromString, convertVisibilityToString } from "@/utils/memo";
import { loadTheme } from "@/utils/theme";
import LocaleSelect from "../LocaleSelect";
import ThemeSelect from "../ThemeSelect";
import VisibilityIcon from "../VisibilityIcon";
import SettingGroup from "./SettingGroup";
import SettingRow from "./SettingRow";
import SettingSection from "./SettingSection";

type LskySyncResult = {
  memoName: string;
  status: string;
  reason?: string;
  attachmentCount?: number;
  uploadedCount?: number;
};

type LskySyncResponse = {
  scannedMemos: number;
  memosWithAttachments: number;
  updatedMemos: number;
  skippedMemos: number;
  uploadedFiles: number;
  results: LskySyncResult[];
};

const PreferencesSection = () => {
  const t = useTranslate();
  const { currentUser, userGeneralSetting: generalSetting, refetchSettings } = useAuth();
  const { mutate: updateUserGeneralSetting } = useUpdateUserGeneralSetting(currentUser?.name);
  const [exportPath, setExportPath] = useState("");
  const [isExporting, setIsExporting] = useState(false);
  const [lskyBaseUrl, setLskyBaseUrl] = useState("https://lsky.wodedata.com/api/v1");
  const [lskyToken, setLskyToken] = useState("");
  const [lskyStrategyId, setLskyStrategyId] = useState("");
  const [isSyncingLsky, setIsSyncingLsky] = useState(false);
  const [lskySummary, setLskySummary] = useState<LskySyncResponse | null>(null);

  const getAuthorizedRequestHeaders = async () => {
    let accessToken = getAccessToken();
    if ((!accessToken || isTokenExpired()) && hasStoredToken()) {
      await refreshAccessToken();
      accessToken = getAccessToken();
    }

    return {
      "Content-Type": "application/json",
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
    };
  };

  const handleLocaleSelectChange = (locale: Locale) => {
    // Apply locale immediately for instant UI feedback and persist to localStorage
    loadLocale(locale);
    // Persist to user settings
    updateUserGeneralSetting(
      { generalSetting: { locale }, updateMask: ["locale"] },
      {
        onSuccess: () => {
          refetchSettings();
        },
      },
    );
  };

  const handleDefaultMemoVisibilityChanged = (value: string) => {
    updateUserGeneralSetting(
      { generalSetting: { memoVisibility: value }, updateMask: ["memo_visibility"] },
      {
        onSuccess: () => {
          refetchSettings();
        },
      },
    );
  };

  const handleThemeChange = (theme: string) => {
    // Apply theme immediately for instant UI feedback
    loadTheme(theme);
    // Persist to user settings
    updateUserGeneralSetting(
      { generalSetting: { theme }, updateMask: ["theme"] },
      {
        onSuccess: () => {
          refetchSettings();
        },
      },
    );
  };

  // Provide default values if setting is not loaded yet
  const setting: UserSetting_GeneralSetting =
    generalSetting ||
    create(UserSetting_GeneralSettingSchema, {
      locale: "en",
      memoVisibility: "PRIVATE",
      theme: "system",
    });

  const handleExportMemos = async () => {
    if (!exportPath.trim()) {
      toast.error(t("setting.preference.memo-export.path-required"));
      return;
    }

    setIsExporting(true);
    try {
      const response = await fetch("/api/v1/memos:export", {
        method: "POST",
        credentials: "include",
        headers: await getAuthorizedRequestHeaders(),
        body: JSON.stringify({
          outputDirectory: exportPath.trim(),
        }),
      });
      if (!response.ok) {
        const error = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(error?.message || `Export failed with status ${response.status}`);
      }
      const data = (await response.json()) as { exportedCount: number; outputDirectory: string };
      toast.success(
        t("setting.preference.memo-export.success", {
          count: data.exportedCount,
          path: data.outputDirectory,
        }),
      );
    } catch (error) {
      await handleError(error, toast.error, { context: "Export memos" });
    } finally {
      setIsExporting(false);
    }
  };

  const handleSyncAttachmentsToLsky = async () => {
    if (!lskyToken.trim()) {
      toast.error(t("setting.preference.lsky-sync.token-required"));
      return;
    }

    setIsSyncingLsky(true);
    try {
      const response = await fetch("/api/v1/memos:sync-attachments-to-lsky", {
        method: "POST",
        credentials: "include",
        headers: await getAuthorizedRequestHeaders(),
        body: JSON.stringify({
          baseUrl: lskyBaseUrl.trim(),
          token: lskyToken.trim(),
          strategyId: lskyStrategyId.trim(),
        }),
      });
      if (!response.ok) {
        const error = (await response.json().catch(() => null)) as { message?: string } | null;
        throw new Error(error?.message || `Lsky sync failed with status ${response.status}`);
      }
      const data = (await response.json()) as LskySyncResponse;
      setLskySummary(data);
      toast.success(
        t("setting.preference.lsky-sync.success", {
          updated: data.updatedMemos,
          skipped: data.skippedMemos,
          uploaded: data.uploadedFiles,
        }),
      );
    } catch (error) {
      await handleError(error, toast.error, { context: "Sync memo attachments to Lsky" });
    } finally {
      setIsSyncingLsky(false);
    }
  };

  return (
    <SettingSection title={t("setting.preference.label")}>
      <SettingGroup title={t("common.basic")}>
        <SettingRow label={t("common.language")}>
          <LocaleSelect value={setting.locale} onChange={handleLocaleSelectChange} />
        </SettingRow>

        <SettingRow label={t("setting.preference.theme")}>
          <ThemeSelect value={setting.theme} onValueChange={handleThemeChange} />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title={t("common.memo")} showSeparator>
        <SettingRow label={t("setting.preference.default-memo-visibility")}>
          <Select value={setting.memoVisibility || "PRIVATE"} onValueChange={handleDefaultMemoVisibilityChanged}>
            <SelectTrigger className="min-w-fit">
              <div className="flex items-center gap-2">
                <VisibilityIcon visibility={convertVisibilityFromString(setting.memoVisibility)} />
                <SelectValue />
              </div>
            </SelectTrigger>
            <SelectContent>
              {[Visibility.PRIVATE, Visibility.PROTECTED, Visibility.PUBLIC]
                .map((v) => convertVisibilityToString(v))
                .map((item) => (
                  <SelectItem key={item} value={item} className="whitespace-nowrap">
                    {t(`memo.visibility.${item.toLowerCase() as Lowercase<typeof item>}`)}
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
        </SettingRow>
      </SettingGroup>

      <SettingGroup
        title={t("setting.preference.memo-export.title")}
        description={t("setting.preference.memo-export.description")}
        showSeparator
      >
        <SettingRow
          label={t("setting.preference.memo-export.path-label")}
          description={t("setting.preference.memo-export.path-description")}
          vertical
        >
          <div className="w-full flex flex-col sm:flex-row gap-2">
            <Input
              value={exportPath}
              onChange={(event) => setExportPath(event.target.value)}
              placeholder={t("setting.preference.memo-export.path-placeholder")}
              className="w-full"
            />
            <Button onClick={handleExportMemos} disabled={isExporting}>
              {isExporting ? t("setting.preference.memo-export.exporting") : t("setting.preference.memo-export.action")}
            </Button>
          </div>
        </SettingRow>
      </SettingGroup>

      <SettingGroup
        title={t("setting.preference.lsky-sync.title")}
        description={t("setting.preference.lsky-sync.description")}
        showSeparator
      >
        <SettingRow label={t("setting.preference.lsky-sync.base-url-label")} vertical>
          <Input
            value={lskyBaseUrl}
            onChange={(event) => setLskyBaseUrl(event.target.value)}
            placeholder={t("setting.preference.lsky-sync.base-url-placeholder")}
            className="w-full"
          />
        </SettingRow>

        <SettingRow label={t("setting.preference.lsky-sync.token-label")} vertical>
          <Input
            type="password"
            value={lskyToken}
            onChange={(event) => setLskyToken(event.target.value)}
            placeholder={t("setting.preference.lsky-sync.token-placeholder")}
            className="w-full"
          />
        </SettingRow>

        <SettingRow
          label={t("setting.preference.lsky-sync.strategy-id-label")}
          description={t("setting.preference.lsky-sync.strategy-id-description")}
          vertical
        >
          <Input
            value={lskyStrategyId}
            onChange={(event) => setLskyStrategyId(event.target.value)}
            placeholder={t("setting.preference.lsky-sync.strategy-id-placeholder")}
            className="w-full"
          />
        </SettingRow>

        <SettingRow
          label={t("setting.preference.lsky-sync.action-label")}
          description={t("setting.preference.lsky-sync.action-description")}
          vertical
        >
          <div className="w-full flex flex-col gap-3">
            <Button onClick={handleSyncAttachmentsToLsky} disabled={isSyncingLsky} className="w-fit">
              {isSyncingLsky ? t("setting.preference.lsky-sync.syncing") : t("setting.preference.lsky-sync.action")}
            </Button>
            {lskySummary && (
              <div className="w-full rounded-md border border-border bg-muted/20 p-3 text-sm">
                <div className="font-medium">
                  {t("setting.preference.lsky-sync.summary", {
                    scanned: lskySummary.scannedMemos,
                    attached: lskySummary.memosWithAttachments,
                    updated: lskySummary.updatedMemos,
                    skipped: lskySummary.skippedMemos,
                    uploaded: lskySummary.uploadedFiles,
                  })}
                </div>
                <div className="mt-2 space-y-1 text-xs text-muted-foreground">
                  {lskySummary.results
                    .filter((item) => item.attachmentCount || item.status === "updated")
                    .slice(0, 20)
                    .map((item) => (
                      <div key={item.memoName} className="break-all">
                        {item.memoName} · {item.status}
                        {item.attachmentCount ? ` · attachments ${item.attachmentCount}` : ""}
                        {item.reason ? ` · ${item.reason}` : ""}
                        {item.uploadedCount ? ` · ${item.uploadedCount}` : ""}
                      </div>
                    ))}
                </div>
              </div>
            )}
          </div>
        </SettingRow>
      </SettingGroup>
    </SettingSection>
  );
};

export default PreferencesSection;
