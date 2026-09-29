package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"xiaozhi-esp32-server-golang/internal/domain/llm"
)

type CaseResult struct {
	Name        string
	Messages    []*schema.Message
	Response    string
	ToolCalls   int
	SkippedCall bool
}

func main() {
	configPath := flag.String("config", "config/config_shijingbo_nondocker.yaml", "配置文件路径")
	timeout := flag.Duration("timeout", 60*time.Second, "LLM请求超时")
	sessionPrefix := flag.String("session_prefix", "interrupt-history-ab", "LLM会话ID前缀")
	dryRun := flag.Bool("dry_run", false, "只打印两种历史组装，不请求LLM")

	firstUser := flag.String("first_user", "帮我介绍杭州", "上一轮已记录的user消息")
	interruptedAssistant := flag.String("assistant_partial", "杭州[用户打断]", "被打断前用户已听到的assistant部分文本")
	nextUser := flag.String("next_user", "给我介绍一下台湾", "下一句用户消息")
	flag.Parse()

	if err := loadConfig(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	systemPrompt := viper.GetString("system_prompt")
	fmt.Printf("验证目标: user消息已记录时，assistant被打断内容是否入历史对下一句对话的影响\n")
	fmt.Printf("前提用户消息: %q\n", *firstUser)
	fmt.Printf("被打断assistant片段: %q\n", *interruptedAssistant)
	fmt.Printf("下一句用户消息: %q\n", *nextUser)

	caseNoAssistant := CaseResult{
		Name:     "Case A: 不记录被打断assistant到历史",
		Messages: buildNextTurnMessages(systemPrompt, *firstUser, *interruptedAssistant, false, *nextUser),
	}
	caseWithAssistant := CaseResult{
		Name:     "Case B: 记录被打断assistant到历史",
		Messages: buildNextTurnMessages(systemPrompt, *firstUser, *interruptedAssistant, true, *nextUser),
	}

	fmt.Println("\n=== 输入对比 ===")
	printCaseInput(caseNoAssistant)
	printCaseInput(caseWithAssistant)

	if *dryRun {
		fmt.Println("\ndry_run=true，已跳过LLM请求。")
		return
	}

	provider, providerLabel, providerCfg, err := buildLLMProviderFromConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化LLM provider失败: %v\n", err)
		os.Exit(1)
	}
	defer provider.Close()

	fmt.Printf("\n使用LLM配置: provider=%s, type=%v, model=%v, base_url=%v\n",
		providerLabel, providerCfg["type"], providerCfg["model_name"], providerCfg["base_url"])

	caseNoAssistant.Response, caseNoAssistant.ToolCalls, err = requestLLM(
		provider,
		*timeout,
		*sessionPrefix+"-no-assistant",
		caseNoAssistant.Name,
		caseNoAssistant.Messages,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "执行 %s 失败: %v\n", caseNoAssistant.Name, err)
		os.Exit(1)
	}

	caseWithAssistant.Response, caseWithAssistant.ToolCalls, err = requestLLM(
		provider,
		*timeout,
		*sessionPrefix+"-with-assistant",
		caseWithAssistant.Name,
		caseWithAssistant.Messages,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "执行 %s 失败: %v\n", caseWithAssistant.Name, err)
		os.Exit(1)
	}

	printSummary(caseNoAssistant, caseWithAssistant)
}

func buildNextTurnMessages(systemPrompt, firstUser, interruptedAssistant string, includeInterruptedAssistant bool, nextUser string) []*schema.Message {
	ret := make([]*schema.Message, 0, 4)
	if strings.TrimSpace(systemPrompt) != "" {
		ret = append(ret, schema.SystemMessage(systemPrompt))
	}
	ret = append(ret, schema.UserMessage(firstUser))

	if includeInterruptedAssistant && strings.TrimSpace(interruptedAssistant) != "" {
		msg := schema.AssistantMessage(interruptedAssistant, nil)
		/*msg.Extra = map[string]any{
			"interrupted": true,
		}*/
		ret = append(ret, msg)
	}

	ret = append(ret, schema.UserMessage(nextUser))
	return ret
}

func printCaseInput(c CaseResult) {
	fmt.Printf("\n%s\n", c.Name)
	for i, msg := range c.Messages {
		if msg == nil {
			fmt.Printf("  %d. <nil>\n", i+1)
			continue
		}
		extra := ""
		if len(msg.Extra) > 0 {
			b, _ := json.Marshal(msg.Extra)
			extra = fmt.Sprintf(" extra=%s", string(b))
		}
		fmt.Printf("  %d. role=%s content=%q%s\n", i+1, msg.Role, msg.Content, extra)
	}
}

func requestLLM(provider llm.LLMProvider, timeout time.Duration, sessionID, caseName string, messages []*schema.Message) (string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fmt.Printf("\n=== %s: LLM响应 ===\n", caseName)
	respChan := provider.ResponseWithContext(ctx, sessionID, messages, nil)

	var builder strings.Builder
	toolCallCount := 0
	for resp := range respChan {
		if resp == nil {
			continue
		}
		if llm.IsLLMErrorMessage(resp) {
			return "", toolCallCount, fmt.Errorf("llm错误: %s", llm.LLMErrorMessage(resp))
		}
		if resp.Content != "" {
			fmt.Print(resp.Content)
			builder.WriteString(resp.Content)
		}
		if len(resp.ToolCalls) > 0 {
			toolCallCount += len(resp.ToolCalls)
			raw, _ := json.Marshal(resp.ToolCalls)
			fmt.Printf("\n[tool_calls] %s\n", string(raw))
		}
	}
	fmt.Println()
	return builder.String(), toolCallCount, nil
}

func printSummary(a, b CaseResult) {
	fmt.Println("\n=== 影响对比（下一句）===")
	fmt.Printf("A(不记录assistant): %s\n", strings.TrimSpace(a.Response))
	fmt.Printf("B(记录assistant): %s\n", strings.TrimSpace(b.Response))

	same := normalizeText(a.Response) == normalizeText(b.Response)
	if same {
		fmt.Println("结论: 两种历史下本次输出相同或近似相同。")
	} else {
		fmt.Println("结论: 两种历史下本次输出有差异，说明是否记录被打断assistant会影响下一句。")
	}
}

func normalizeText(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}

func loadConfig(configPath string) error {
	viper.SetConfigFile(configPath)
	return viper.ReadInConfig()
}

func buildLLMProviderFromConfig() (llm.LLMProvider, string, map[string]interface{}, error) {
	providerLabel := viper.GetString("llm.provider")
	if providerLabel == "" {
		return nil, "", nil, fmt.Errorf("配置缺少 llm.provider")
	}

	providerCfg := viper.GetStringMap("llm." + providerLabel)
	if len(providerCfg) == 0 {
		return nil, "", nil, fmt.Errorf("未找到 llm.%s 配置", providerLabel)
	}

	normalizeLLMConfig(providerCfg)
	provider, err := llm.GetLLMProvider(providerLabel, providerCfg)
	if err != nil {
		return nil, "", nil, err
	}
	return provider, providerLabel, providerCfg, nil
}

func normalizeLLMConfig(cfg map[string]interface{}) {
	if _, ok := cfg["max_tokens"]; !ok {
		if raw, ok := cfg["max_token"]; ok {
			if v, convOK := toInt(raw); convOK {
				cfg["max_tokens"] = v
			}
		}
	}
	if _, ok := cfg["streamable"]; !ok {
		cfg["streamable"] = true
	}
}

func toInt(v interface{}) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int8:
		return int(t), true
	case int16:
		return int(t), true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float32:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}
