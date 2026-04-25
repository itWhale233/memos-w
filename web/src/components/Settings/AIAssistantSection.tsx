import { ChevronDownIcon, ChevronRightIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "react-hot-toast";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useInstance } from "@/contexts/InstanceContext";
import { useTagCounts } from "@/hooks/useUserQueries";
import { handleError } from "@/lib/error";
import { InstanceSetting_AIProviderType } from "@/types/proto/api/v1/instance_service_pb";
import { useTranslate } from "@/utils/i18n";
import SettingGroup from "./SettingGroup";
import SettingRow from "./SettingRow";
import SettingSection from "./SettingSection";

type ExternalAdapter = {
  id: string;
  type: string;
  enabled: boolean;
  display_name: string;
  secret?: string;
  secret_hint?: string;
};

type AssistantConfig = {
  enabled: boolean;
  bot_user: string;
  provider_id: string;
  rule_groups: {
    id: string;
    name: string;
    tags: string[];
    persona_prompt: string;
    system_prompt: string;
  }[];
  max_context_comments: number;
  reply_model: string;
  external_action_adapters: ExternalAdapter[];
};

type AssistantLogEntry = {
  time: string;
  level: string;
  stage: string;
  status: string;
  event_type?: string;
  memo?: string;
  target?: string;
  provider_id?: string;
  model?: string;
  message: string;
  detail?: Record<string, unknown>;
};

const defaultConfig: AssistantConfig = {
  enabled: false,
  bot_user: "",
  provider_id: "",
  rule_groups: [],
  max_context_comments: 10,
  reply_model: "",
  external_action_adapters: [
    {
      id: "ticktick-default",
      type: "ticktick",
      enabled: false,
      display_name: "TickTick",
      secret: "",
      secret_hint: "",
    },
  ],
};

const createLocalId = () => {
  if (typeof globalThis !== "undefined" && globalThis.crypto && typeof globalThis.crypto.randomUUID === "function") {
    return globalThis.crypto.randomUUID();
  }
  return `rule-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
};

const normalizeConfig = (value: Partial<AssistantConfig> | null | undefined): AssistantConfig => {
  const merged = { ...defaultConfig, ...(value ?? {}) } as AssistantConfig;
  return {
    ...merged,
    rule_groups: Array.isArray(value?.rule_groups)
      ? value.rule_groups.map((group) => ({
          id: group?.id ?? createLocalId(),
          name: group?.name ?? "",
          tags: Array.isArray(group?.tags) ? group.tags.filter(Boolean) : [],
          persona_prompt: group?.persona_prompt ?? "",
          system_prompt: group?.system_prompt ?? "",
        }))
      : defaultConfig.rule_groups,
    external_action_adapters: Array.isArray(value?.external_action_adapters)
      ? value.external_action_adapters.map((adapter) => ({
          id: adapter?.id ?? "",
          type: adapter?.type ?? "",
          enabled: !!adapter?.enabled,
          display_name: adapter?.display_name ?? "",
          secret: adapter?.secret ?? "",
          secret_hint: adapter?.secret_hint ?? "",
        }))
      : defaultConfig.external_action_adapters,
  };
};

const AIAssistantSection = () => {
  const t = useTranslate();
  const { aiSetting } = useInstance();
  const { data: tagCounts = {} } = useTagCounts(false);
  const [config, setConfig] = useState<AssistantConfig>(defaultConfig);
  const [initialConfig, setInitialConfig] = useState<AssistantConfig>(defaultConfig);
  const [logs, setLogs] = useState<AssistantLogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [ruleTagSearch, setRuleTagSearch] = useState<Record<string, string>>({});
  const [expandedRules, setExpandedRules] = useState<Record<string, boolean>>({});

  const fetchLogs = async () => {
    const logsResponse = await fetch("/api/v1/ai-assistant/logs", { credentials: "include" });
    if (!logsResponse.ok) {
      throw new Error(await logsResponse.text());
    }
    const payload = (await logsResponse.json()) as { logs?: AssistantLogEntry[] };
    setLogs(Array.isArray(payload.logs) ? payload.logs.slice().reverse() : []);
  };

  useEffect(() => {
    const fetchConfig = async () => {
      try {
        const configResponse = await fetch("/api/v1/ai-assistant", { credentials: "include" });
        if (!configResponse.ok) {
          throw new Error(await configResponse.text());
        }
        const data = (await configResponse.json()) as AssistantConfig;
        const normalized = normalizeConfig(data);
        setConfig(normalized);
        setInitialConfig(normalized);
        await fetchLogs();
      } catch (error) {
        handleError(error, toast.error, { context: "Load AI assistant settings" });
      } finally {
        setLoading(false);
      }
    };
    fetchConfig();
  }, []);

  const providerOptions = useMemo(() => aiSetting.providers ?? [], [aiSetting.providers]);
  const availableTags = useMemo(() => Object.keys(tagCounts).sort(), [tagCounts]);
  const hasChanges = JSON.stringify(config) !== JSON.stringify(initialConfig);
  const allRuleTags = new Map<string, string>();
  for (const rule of config.rule_groups) {
    for (const tag of rule.tags) {
      if (!allRuleTags.has(tag)) {
        allRuleTags.set(tag, rule.id);
      }
    }
  }

  const updateConfig = (partial: Partial<AssistantConfig>) => {
    setConfig((prev) => ({ ...prev, ...partial }));
  };

  const updateAdapter = (adapterID: string, partial: Partial<ExternalAdapter>) => {
    setConfig((prev) => ({
      ...prev,
      external_action_adapters: prev.external_action_adapters.map((adapter) =>
        adapter.id === adapterID ? { ...adapter, ...partial } : adapter,
      ),
    }));
  };

  const updateRuleGroup = (ruleID: string, partial: Partial<AssistantConfig["rule_groups"][number]>) => {
    setConfig((prev) => ({
      ...prev,
      rule_groups: prev.rule_groups.map((rule) => (rule.id === ruleID ? { ...rule, ...partial } : rule)),
    }));
  };

  const addTagToRuleGroup = (ruleID: string, tag: string) => {
    setConfig((prev) => ({
      ...prev,
      rule_groups: prev.rule_groups.map((rule) => {
        if (rule.id !== ruleID || rule.tags.includes(tag)) {
          return rule;
        }
        return { ...rule, tags: [...rule.tags, tag] };
      }),
    }));
    setRuleTagSearch((prev) => ({ ...prev, [ruleID]: "" }));
  };

  const addRuleGroup = () => {
    const id = createLocalId();
    setConfig((prev) => ({
      ...prev,
      rule_groups: [
        ...prev.rule_groups,
        {
          id,
          name: `Rule ${prev.rule_groups.length + 1}`,
          tags: [],
          persona_prompt: "",
          system_prompt: "",
        },
      ],
    }));
    setExpandedRules((prev) => ({ ...prev, [id]: true }));
  };

  const removeRuleGroup = (ruleID: string) => {
    setConfig((prev) => ({
      ...prev,
      rule_groups: prev.rule_groups.filter((rule) => rule.id !== ruleID),
    }));
  };

  const handleSave = async () => {
    try {
      const tagUsage = new Map<string, string>();
      for (const rule of config.rule_groups) {
        if (!rule.name.trim()) {
          toast.error("Rule group name is required.");
          return;
        }
        if (rule.tags.length === 0) {
          toast.error(`Rule group \"${rule.name || rule.id}\" must contain at least one tag.`);
          return;
        }
        for (const tag of rule.tags) {
          if (tagUsage.has(tag) && tagUsage.get(tag) !== rule.id) {
            toast.error(`Tag \"${tag}\" is already used by another rule group.`);
            return;
          }
          tagUsage.set(tag, rule.id);
        }
      }
      const response = await fetch("/api/v1/ai-assistant", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify(config),
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => ({ message: "Save failed" }));
        throw new Error(payload.message || "Save failed");
      }
      const data = (await response.json()) as AssistantConfig;
      const normalized = normalizeConfig(data);
      setConfig(normalized);
      setInitialConfig(normalized);
      toast.success(t("message.update-succeed"));
    } catch (error) {
      handleError(error, toast.error, { context: "Save AI assistant settings" });
    }
  };

  return (
      <SettingSection title={t("setting.ai-assistant.label") as string}>
        <SettingGroup title={t("setting.ai-assistant.section-title") as string} description={t("setting.ai-assistant.description") as string}>
        <SettingRow label={t("setting.ai-assistant.enabled") as string}>
          <Switch checked={config.enabled} onCheckedChange={(checked) => updateConfig({ enabled: checked })} />
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.bot-user") as string} vertical>
          <Input
            value={config.bot_user}
            placeholder={t("setting.ai-assistant.bot-user-placeholder") as string}
            onChange={(e) => updateConfig({ bot_user: e.target.value })}
          />
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.provider") as string}>
          <Select value={config.provider_id} onValueChange={(value) => updateConfig({ provider_id: value })}>
            <SelectTrigger className="min-w-[220px]">
              <SelectValue placeholder={t("setting.ai-assistant.select-provider") as string} />
            </SelectTrigger>
            <SelectContent>
              {providerOptions.map((provider) => (
                <SelectItem key={provider.id} value={provider.id}>
                  {provider.title || provider.id} ({InstanceSetting_AIProviderType[provider.type]})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.reply-model") as string} vertical>
          <Input value={config.reply_model} placeholder="gpt-4o-mini" onChange={(e) => updateConfig({ reply_model: e.target.value })} />
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.max-context-comments") as string}>
          <Input
            type="number"
            className="w-28"
            value={config.max_context_comments}
            onChange={(e) => updateConfig({ max_context_comments: Number(e.target.value) || 10 })}
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title={t("setting.ai-assistant.rule-groups") as string} showSeparator>
        <div className="space-y-4">
          {config.rule_groups.map((rule) => {
            const filteredTags = availableTags
              .filter((tag) => !rule.tags.includes(tag))
              .filter((tag) => {
                const owner = allRuleTags.get(tag);
                return !owner || owner === rule.id;
              })
              .filter((tag) => tag.toLowerCase().includes((ruleTagSearch[rule.id] ?? "").trim().toLowerCase()))
              .sort((a, b) => a.localeCompare(b));
            return (
              <div key={rule.id} className="rounded-lg border border-border p-4 space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <button
                    type="button"
                    className="flex min-w-0 flex-1 items-center gap-2 text-left"
                    onClick={() => setExpandedRules((prev) => ({ ...prev, [rule.id]: !prev[rule.id] }))}
                  >
                    {expandedRules[rule.id] ? <ChevronDownIcon className="w-4 h-4 shrink-0" /> : <ChevronRightIcon className="w-4 h-4 shrink-0" />}
                    <div className="min-w-0">
                      <div className="truncate font-medium text-foreground">{rule.name || `Rule ${rule.id}`}</div>
                      <div className="truncate text-xs text-muted-foreground">
                        {rule.tags.length > 0 ? rule.tags.join(", ") : t("setting.ai-assistant.no-tags-selected")}
                      </div>
                    </div>
                  </button>
                  <Button variant="outline" size="sm" onClick={() => removeRuleGroup(rule.id)}>
                    {t("common.delete")}
                  </Button>
                </div>
                {expandedRules[rule.id] && (
                  <>
                <div className="space-y-1.5">
                  <Label>{t("common.name")}</Label>
                  <Input value={rule.name} onChange={(e) => updateRuleGroup(rule.id, { name: e.target.value })} />
                </div>
                <div className="space-y-1.5">
                  <Label>{t("common.tags")}</Label>
                  <div className="rounded-md border border-border p-3 space-y-3">
                    <Input
                      placeholder={t("setting.ai-assistant.search-tags") as string}
                      value={ruleTagSearch[rule.id] ?? ""}
                      onChange={(e) => setRuleTagSearch((prev) => ({ ...prev, [rule.id]: e.target.value }))}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" && filteredTags.length > 0) {
                          e.preventDefault();
                          addTagToRuleGroup(rule.id, filteredTags[0]);
                        }
                      }}
                    />
                    {(ruleTagSearch[rule.id] ?? "").trim() !== "" && (
                      <div className="max-h-40 overflow-auto rounded border border-border bg-background">
                        {filteredTags.length === 0 ? (
                          <div className="px-3 py-2 text-sm text-muted-foreground">{t("setting.ai-assistant.no-search-results") as string}</div>
                        ) : (
                          filteredTags.slice(0, 8).map((tag) => (
                            <button
                              key={tag}
                              type="button"
                              className="flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-muted/60"
                              onClick={() => addTagToRuleGroup(rule.id, tag)}
                            >
                              <span>{tag}</span>
                              <span className="text-xs text-muted-foreground">Enter</span>
                            </button>
                          ))
                        )}
                      </div>
                    )}
                    <div className="flex flex-wrap gap-2">
                      {rule.tags.length === 0 ? (
                        <span className="text-sm text-muted-foreground">{t("setting.ai-assistant.no-tags-selected")}</span>
                      ) : (
                        rule.tags.map((tag) => (
                          <button
                            key={tag}
                            type="button"
                            className="rounded-full border border-border bg-background px-3 py-1 text-sm"
                            onClick={() => updateRuleGroup(rule.id, { tags: rule.tags.filter((item) => item !== tag) })}
                          >
                            {tag} ×
                          </button>
                        ))
                      )}
                    </div>
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label>{t("setting.ai-assistant.persona-prompt") as string}</Label>
                  <Textarea className="max-h-[22.5rem] overflow-y-auto resize-y" rows={6} value={rule.persona_prompt} onChange={(e) => updateRuleGroup(rule.id, { persona_prompt: e.target.value })} />
                </div>
                <div className="space-y-1.5">
                  <Label>{t("setting.ai-assistant.system-prompt") as string}</Label>
                  <Textarea className="max-h-[22.5rem] overflow-y-auto resize-y" rows={6} value={rule.system_prompt} onChange={(e) => updateRuleGroup(rule.id, { system_prompt: e.target.value })} />
                </div>
                  </>
                )}
              </div>
            );
          })}
          <Button variant="outline" onClick={addRuleGroup}>
            {t("setting.ai-assistant.add-rule-group") as string}
          </Button>
        </div>
      </SettingGroup>

      <SettingGroup title={t("setting.ai-assistant.external-adapters") as string} showSeparator>
        {(config.external_action_adapters ?? []).map((adapter) => (
          <div key={adapter.id} className="rounded-lg border border-border p-3 space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="font-medium text-foreground">{adapter.display_name || adapter.id}</p>
                <p className="text-xs text-muted-foreground">{adapter.type}</p>
              </div>
              <Switch checked={adapter.enabled} onCheckedChange={(checked) => updateAdapter(adapter.id, { enabled: checked })} />
            </div>
            <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
              <div className="space-y-1.5">
                <Label>{t("setting.ai-assistant.display-name") as string}</Label>
                <Input value={adapter.display_name} onChange={(e) => updateAdapter(adapter.id, { display_name: e.target.value })} />
              </div>
              <div className="space-y-1.5">
                <Label>{t("setting.ai-assistant.secret") as string}</Label>
                <Input
                  type="password"
                  value={adapter.secret || ""}
                  placeholder={adapter.secret_hint || (t("setting.ai-assistant.secret-placeholder") as string)}
                  onChange={(e) => updateAdapter(adapter.id, { secret: e.target.value })}
                />
              </div>
            </div>
          </div>
        ))}
      </SettingGroup>

      <SettingGroup title={t("setting.ai-assistant.execution-logs") as string} showSeparator>
        <div className="mb-3 flex justify-end">
          <Button
            variant="outline"
            size="sm"
            onClick={async () => {
              try {
                await fetchLogs();
              } catch (error) {
                handleError(error, toast.error, { context: "Refresh AI assistant logs" });
              }
            }}
          >
            {t("setting.ai-assistant.refresh-logs") as string}
          </Button>
        </div>
        <div className="max-h-96 overflow-auto rounded-lg border border-border bg-muted/20">
          {logs.length === 0 ? (
            <div className="px-4 py-6 text-sm text-muted-foreground">{t("setting.ai-assistant.no-logs") as string}</div>
          ) : (
            <div className="divide-y divide-border">
              {logs.map((log, index) => (
                <div key={`${log.time}-${index}`} className="px-4 py-3 text-sm">
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <span>{new Date(log.time).toLocaleString()}</span>
                    <span className="rounded border border-border px-1.5 py-0.5 uppercase">{log.level}</span>
                    <span className="rounded border border-border px-1.5 py-0.5">{log.stage}</span>
                    <span className="rounded border border-border px-1.5 py-0.5">{log.status}</span>
                    {log.event_type && <span>{log.event_type}</span>}
                  </div>
                  <div className="mt-1 font-medium text-foreground">{log.message}</div>
                  <div className="mt-1 space-y-1 text-xs text-muted-foreground">
                    {log.memo && <div>{t("common.memo")}: {log.memo}</div>}
                    {log.target && <div>{t("setting.ai-assistant.target-label") as string}: {log.target}</div>}
                    {log.provider_id && <div>{t("setting.ai-assistant.provider") as string}: {log.provider_id}</div>}
                    {log.model && <div>{t("setting.ai-assistant.model-label") as string}: {log.model}</div>}
                    {log.detail && <pre className="whitespace-pre-wrap rounded bg-background p-2 text-[11px]">{JSON.stringify(log.detail, null, 2)}</pre>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </SettingGroup>

      <div className="w-full flex justify-end">
        <Button disabled={loading || !hasChanges} onClick={handleSave}>
          {t("common.save")}
        </Button>
      </div>
    </SettingSection>
  );
};

export default AIAssistantSection;
