import { useEffect, useState } from "react";

type AIConfig = {
  bot_user?: string;
  enabled?: boolean;
};

const useAIConfig = () => {
  const [config, setConfig] = useState<AIConfig>({});

  useEffect(() => {
    let cancelled = false;
    const fetchProfile = async () => {
      try {
        const response = await fetch("/api/v1/ai-assistant/profile", { credentials: "include" });
        if (!response.ok) {
          return;
        }
        const data = (await response.json()) as AIConfig;
        if (!cancelled) {
          setConfig(data);
        }
      } catch {
        // Ignore background AI config fetch errors in display hooks.
      }
    };
    fetchProfile();
    return () => {
      cancelled = true;
    };
  }, []);

  return config;
};

export default useAIConfig;
