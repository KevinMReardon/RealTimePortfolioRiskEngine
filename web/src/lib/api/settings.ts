import { apiFetch } from "@/lib/api/client";

export type SettingType = "bool" | "int" | "number" | "string" | "select";

export interface SettingDef {
  key: string;
  label: string;
  description: string;
  group: string;
  type: SettingType;
  default: boolean | number | string;
  options?: string[];
  requires_restart: boolean;
  value: boolean | number | string;
}

export interface SettingsListResponse {
  settings: SettingDef[];
}

export interface SettingsPatchResponse {
  updated: string[];
}

export async function fetchSettings(): Promise<SettingsListResponse> {
  // Cache-bust aggressively so manual Refresh always pulls current backend state.
  return apiFetch<SettingsListResponse>(`/v1/settings?_ts=${Date.now()}`);
}

export async function patchSettings(
  changes: Record<string, boolean | number | string>,
): Promise<SettingsPatchResponse> {
  return apiFetch<SettingsPatchResponse>("/v1/settings", {
    method: "PATCH",
    body: JSON.stringify({ settings: changes }),
  });
}
