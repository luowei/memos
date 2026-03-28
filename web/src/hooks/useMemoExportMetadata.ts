import { useQuery } from "@tanstack/react-query";
import { getAccessToken, hasStoredToken, isTokenExpired } from "@/auth-state";
import { refreshAccessToken } from "@/connect";

interface MemoExportMetadata {
  exportTs?: number;
}

export const memoExportMetadataKeys = {
  all: ["memo-export-metadata"] as const,
  detail: (memoName: string) => [...memoExportMetadataKeys.all, memoName] as const,
};

export function useMemoExportMetadata(memoName: string) {
  return useQuery({
    queryKey: memoExportMetadataKeys.detail(memoName),
    queryFn: async () => {
      let accessToken = getAccessToken();
      if (!accessToken && hasStoredToken()) {
        await refreshAccessToken();
        accessToken = getAccessToken();
      } else if (accessToken && isTokenExpired()) {
        await refreshAccessToken();
        accessToken = getAccessToken();
      }

      const memoUID = memoName.replace(/^memos\//, "");
      const response = await fetch(`/api/v1/memos/${encodeURIComponent(memoUID)}/export-metadata`, {
        headers: {
          "Content-Type": "application/json",
          ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
        },
      });
      if (response.status === 401 || response.status === 403 || response.status === 404) {
        return {} as MemoExportMetadata;
      }
      if (!response.ok) {
        throw new Error("Failed to fetch memo export metadata");
      }
      return (await response.json()) as MemoExportMetadata;
    },
    enabled: !!memoName,
    staleTime: 1000 * 60,
  });
}
