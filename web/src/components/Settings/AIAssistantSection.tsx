import { useEffect, useMemo, useState } from "react";
import { toast } from "react-hot-toast";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useInstance } from "@/contexts/InstanceContext";
import { handleError } from "@/lib/error";
import { InstanceSetting_AIProviderType } from "@/types/proto/api/v1/instance_service_pb";
import { useTranslate } from "@/utils/i18n";
import SettingGroup from "./SettingGroup";
import SettingRow from "./SettingRow";
import SettingSection from "./SettingSection";

type ActionSetting = {
  enabled: boolean;
  type: string;
  adapter_id?: string;
};

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
  persona_prompt: string;
  system_prompt: string;
  trigger_filter: string;
  watch_memo_create: boolean;
  watch_memo_update: boolean;
  watch_comment_create: boolean;
  max_context_comments: number;
  classify_model: string;
  reply_model: string;
  question_action: ActionSetting;
  emotion_action: ActionSetting;
  todo_action: ActionSetting;
  external_action_adapters: ExternalAdapter[];
};

const defaultConfig: AssistantConfig = {
  enabled: false,
  bot_user: "",
  provider_id: "",
  persona_prompt: "你是一个温和、克制、实用的中文笔记助手。",
  system_prompt: "回答必须准确，不要编造事实；优先短答。",
  trigger_filter: "",
  watch_memo_create: true,
  watch_memo_update: true,
  watch_comment_create: true,
  max_context_comments: 10,
  classify_model: "",
  reply_model: "",
  question_action: { enabled: true, type: "reply_comment" },
  emotion_action: { enabled: true, type: "reply_comment" },
  todo_action: { enabled: true, type: "external_todo", adapter_id: "ticktick-default" },
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

const AIAssistantSection = () => {
  const t = useTranslate();
  const { aiSetting } = useInstance();
  const [config, setConfig] = useState<AssistantConfig>(defaultConfig);
  const [initialConfig, setInitialConfig] = useState<AssistantConfig>(defaultConfig);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchConfig = async () => {
      try {
        const response = await fetch("/api/v1/ai-assistant", { credentials: "include" });
        if (!response.ok) {
          throw new Error(await response.text());
        }
        const data = (await response.json()) as AssistantConfig;
        setConfig({ ...defaultConfig, ...data });
        setInitialConfig({ ...defaultConfig, ...data });
      } catch (error) {
        handleError(error, toast.error, { context: "Load AI assistant settings" });
      } finally {
        setLoading(false);
      }
    };
    fetchConfig();
  }, []);

  const providerOptions = useMemo(() => aiSetting.providers ?? [], [aiSetting.providers]);
  const hasChanges = JSON.stringify(config) !== JSON.stringify(initialConfig);

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

  const handleSave = async () => {
    try {
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
      setConfig({ ...defaultConfig, ...data });
      setInitialConfig({ ...defaultConfig, ...data });
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

        <SettingRow label={t("setting.ai-assistant.classify-model") as string} vertical>
          <Input value={config.classify_model} placeholder="gpt-4o-mini" onChange={(e) => updateConfig({ classify_model: e.target.value })} />
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.trigger-filter") as string} vertical>
          <Textarea
            rows={3}
            className="font-mono"
            placeholder={'tags.exists(t, t == "疑问") || has_incomplete_tasks == true'}
            value={config.trigger_filter}
            onChange={(e) => updateConfig({ trigger_filter: e.target.value })}
          />
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.persona-prompt") as string} vertical>
          <Textarea rows={4} value={config.persona_prompt} onChange={(e) => updateConfig({ persona_prompt: e.target.value })} />
        </SettingRow>

        <SettingRow label={t("setting.ai-assistant.system-prompt") as string} vertical>
          <Textarea rows={4} value={config.system_prompt} onChange={(e) => updateConfig({ system_prompt: e.target.value })} />
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

      <SettingGroup title={t("setting.ai-assistant.triggers") as string} showSeparator>
        <SettingRow label={t("setting.ai-assistant.watch-memo-create") as string}>
          <Switch checked={config.watch_memo_create} onCheckedChange={(checked) => updateConfig({ watch_memo_create: checked })} />
        </SettingRow>
        <SettingRow label={t("setting.ai-assistant.watch-memo-update") as string}>
          <Switch checked={config.watch_memo_update} onCheckedChange={(checked) => updateConfig({ watch_memo_update: checked })} />
        </SettingRow>
        <SettingRow label={t("setting.ai-assistant.watch-comment-create") as string}>
          <Switch checked={config.watch_comment_create} onCheckedChange={(checked) => updateConfig({ watch_comment_create: checked })} />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title={t("setting.ai-assistant.external-adapters") as string} showSeparator>
        {config.external_action_adapters.map((adapter) => (
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

      <div className="w-full flex justify-end">
        <Button disabled={loading || !hasChanges} onClick={handleSave}>
          {t("common.save")}
        </Button>
      </div>
    </SettingSection>
  );
};

export default AIAssistantSection;
