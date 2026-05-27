"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertTriangle, Check, RefreshCw } from "lucide-react";

import { fetchSettings, patchSettings, type SettingDef } from "@/lib/api/settings";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

// Simple toggle built from a button to avoid needing an extra shadcn component.
function Toggle({
  checked,
  onCheckedChange,
  id,
}: {
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
  id: string;
}) {
  return (
    <button
      id={id}
      role="switch"
      aria-checked={checked}
      type="button"
      onClick={() => onCheckedChange(!checked)}
      className={[
        "relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent",
        "transition-colors duration-200 ease-in-out focus-visible:outline-none focus-visible:ring-2",
        "focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        checked ? "bg-primary" : "bg-input",
      ].join(" ")}
    >
      <span
        className={[
          "pointer-events-none inline-block h-5 w-5 rounded-full bg-background shadow ring-0",
          "transition duration-200 ease-in-out",
          checked ? "translate-x-5" : "translate-x-0",
        ].join(" ")}
      />
    </button>
  );
}

type LocalValues = Record<string, boolean | number | string>;

function SettingRow({
  setting,
  local,
  onChange,
}: {
  setting: SettingDef;
  local: LocalValues;
  onChange: (key: string, value: boolean | number | string) => void;
}) {
  const current = setting.key in local ? local[setting.key] : setting.value;
  const isDirty = current !== setting.value;

  return (
    <div className="flex items-start justify-between gap-4 py-4 border-b last:border-b-0">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <Label htmlFor={setting.key} className="text-sm font-medium leading-none">
            {setting.label}
          </Label>
          {isDirty && (
            <Badge variant="outline" className="text-xs px-1.5 py-0 h-4">
              unsaved
            </Badge>
          )}
          {setting.requires_restart && (
            <Badge variant="outline" className="text-xs px-1.5 py-0 h-4 text-muted-foreground">
              restart required
            </Badge>
          )}
        </div>
        <p className="mt-1 text-xs text-muted-foreground leading-relaxed">
          {setting.description}
        </p>
      </div>

      <div className="shrink-0 flex items-center">
        {setting.type === "bool" && (
          <Toggle
            id={setting.key}
            checked={Boolean(current)}
            onCheckedChange={(v) => onChange(setting.key, v)}
          />
        )}
        {setting.type === "int" && (
          <Input
            id={setting.key}
            type="number"
            value={String(current)}
            className="w-24 text-right"
            onChange={(e) => {
              const n = parseInt(e.target.value, 10);
              if (!isNaN(n)) onChange(setting.key, n);
            }}
          />
        )}
        {setting.type === "number" && (
          <Input
            id={setting.key}
            type="number"
            step="any"
            value={String(current)}
            className="w-28 text-right"
            onChange={(e) => {
              const n = parseFloat(e.target.value);
              if (!isNaN(n)) onChange(setting.key, n);
            }}
          />
        )}
        {setting.type === "string" && (
          <Input
            id={setting.key}
            type="text"
            value={String(current)}
            className="w-48"
            onChange={(e) => onChange(setting.key, e.target.value)}
          />
        )}
        {setting.type === "select" && setting.options && (
          <Select
            value={String(current)}
            onValueChange={(v) => onChange(setting.key, v)}
          >
            <SelectTrigger className="w-36" id={setting.key}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {setting.options.map((opt) => (
                <SelectItem key={opt} value={opt}>
                  {opt}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
    </div>
  );
}

function SettingGroup({
  name,
  settings,
  local,
  onChange,
}: {
  name: string;
  settings: SettingDef[];
  local: LocalValues;
  onChange: (key: string, value: boolean | number | string) => void;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle>{name}</CardTitle>
        <CardDescription>
          {name === "Agent" && "Controls AI briefing behaviour, execution mode, and model parameters."}
          {name === "Policy" && "Trading safety controls. Changes take effect on the next server restart."}
          {name === "Price Feed" && "Automated market data polling configuration."}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {settings.map((s) => (
          <SettingRow key={s.key} setting={s} local={local} onChange={onChange} />
        ))}
      </CardContent>
    </Card>
  );
}

export function SettingsPage() {
  const [settings, setSettings] = useState<SettingDef[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [local, setLocal] = useState<LocalValues>({});
  const [saving, setSaving] = useState(false);
  const [savedKeys, setSavedKeys] = useState<string[]>([]);
  const [saveError, setSaveError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoadError(null);
    try {
      setRefreshing(true);
      const res = await fetchSettings();
      setSettings(res.settings);
      setLocal({});
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : "Failed to load settings");
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const groups = useMemo(() => {
    if (!settings) return {};
    const g: Record<string, SettingDef[]> = {};
    for (const s of settings) {
      if (!g[s.group]) g[s.group] = [];
      g[s.group].push(s);
    }
    return g;
  }, [settings]);

  const dirtyKeys = useMemo(() => {
    if (!settings) return [];
    return settings
      .filter((s) => s.key in local && local[s.key] !== s.value)
      .map((s) => s.key);
  }, [settings, local]);

  function handleChange(key: string, value: boolean | number | string) {
    setLocal((prev) => ({ ...prev, [key]: value }));
    setSavedKeys([]);
    setSaveError(null);
  }

  async function handleSave() {
    if (dirtyKeys.length === 0) return;
    setSaving(true);
    setSaveError(null);
    setSavedKeys([]);
    const changes: Record<string, boolean | number | string> = {};
    for (const key of dirtyKeys) {
      changes[key] = local[key];
    }
    try {
      const res = await patchSettings(changes);
      setSavedKeys(res.updated ?? []);
      // Refresh to show persisted values
      await load();
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Failed to save settings");
    } finally {
      setSaving(false);
    }
  }

  if (loadError) {
    return (
      <Alert variant="destructive" className="max-w-4xl mx-auto mt-6">
        <AlertTriangle className="h-4 w-4" />
        <AlertDescription>{loadError}</AlertDescription>
      </Alert>
    );
  }

  if (!settings) {
    return (
      <div className="max-w-4xl mx-auto space-y-4">
        {[1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-48 w-full rounded-xl" />
        ))}
      </div>
    );
  }

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            Secrets (API keys, database credentials) stay in{" "}
            <code className="text-xs bg-muted px-1 py-0.5 rounded">.env</code>.
            All other settings are stored in the database, override{" "}
            <code className="text-xs bg-muted px-1 py-0.5 rounded">.env</code>{" "}
            on startup, and take effect immediately unless marked{" "}
            <span className="font-medium">restart required</span>.
          </p>
        </div>
        <Button
          onClick={() => void load()}
          variant="outline"
          size="sm"
          className="gap-1.5"
          disabled={refreshing}
        >
          <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} />
          {refreshing ? "Refreshing…" : "Refresh"}
        </Button>
      </div>

      {savedKeys.length > 0 && (
        <Alert>
          <Check className="h-4 w-4" />
          <AlertDescription>
            Saved {savedKeys.length} setting{savedKeys.length !== 1 ? "s" : ""}.
            Restart the server to apply changes.
          </AlertDescription>
        </Alert>
      )}

      {saveError && (
        <Alert variant="destructive">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription>{saveError}</AlertDescription>
        </Alert>
      )}

      {Object.entries(groups).map(([name, groupSettings]) => (
        <SettingGroup
          key={name}
          name={name}
          settings={groupSettings}
          local={local}
          onChange={handleChange}
        />
      ))}

      {dirtyKeys.length > 0 && (
        <div className="sticky bottom-4 flex justify-end">
          <div className="flex items-center gap-3 rounded-lg border bg-background/95 backdrop-blur px-4 py-3 shadow-lg">
            <span className="text-sm text-muted-foreground">
              {dirtyKeys.length} unsaved change{dirtyKeys.length !== 1 ? "s" : ""}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => { setLocal({}); setSaveError(null); }}
            >
              Discard
            </Button>
            <Button size="sm" onClick={() => void handleSave()} disabled={saving}>
              {saving ? "Saving…" : "Save changes"}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
