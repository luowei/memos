import { useQuery } from "@tanstack/react-query";
import { getAccessToken, hasStoredToken, isTokenExpired } from "@/auth-state";
import { refreshAccessToken } from "@/connect";

export type GitHubSyncSetting = {
  hasToken: boolean;
  owner: string;
  repo: string;
  branch: string;
  apiBaseUrl: string;
  tokenHint?: string;
  hideMemoAction: boolean;
  secondBrainBaseUrl: string;
  hasSecondBrainSharedSecret: boolean;
  secondBrainSharedSecretHint?: string;
};

export const gitHubSyncSettingKeys = {
  detail: () => ["github-sync-setting"] as const,
};

export const getAuthorizedJSONHeaders = async () => {
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

export const fetchGitHubSyncSetting = async () => {
  const response = await fetch("/api/v1/integrations/github-sync", {
    credentials: "include",
    headers: await getAuthorizedJSONHeaders(),
  });
  if (!response.ok) {
    throw new Error(`Failed to fetch GitHub sync setting with status ${response.status}`);
  }
  return (await response.json()) as GitHubSyncSetting;
};

export const useGitHubSyncSetting = () => {
  return useQuery({
    queryKey: gitHubSyncSettingKeys.detail(),
    queryFn: fetchGitHubSyncSetting,
    staleTime: 60_000,
  });
};
