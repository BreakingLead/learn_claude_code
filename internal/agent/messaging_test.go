package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessagingNormalizeFeishuPayload(t *testing.T) {
	module := &messagingModule{}
	if err := module.Init(ModuleContext{}); err != nil {
		t.Fatal(err)
	}

	result := module.runMessagingNormalize([]byte(`{
  "platform": "feishu",
  "payload": {
    "header": {"event_id": "evt_1", "create_time": "1700000000000"},
    "event": {
      "sender": {"sender_id": {"open_id": "ou_abc"}, "sender_type": "user"},
      "message": {"message_id": "om_1", "chat_id": "oc_1", "message_type": "text", "content": "{\"text\":\"侦查检定\"}"}
    }
  }
}`))
	var msg unifiedMessage
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		t.Fatalf("normalize result is not json: %s", result)
	}
	if msg.Platform != "feishu" || msg.ChatID != "oc_1" || msg.SenderID != "ou_abc" || msg.Text != "侦查检定" {
		t.Fatalf("unexpected unified message: %+v", msg)
	}
}

func TestMessagingNormalizeTelegramPayload(t *testing.T) {
	module := &messagingModule{}
	if err := module.Init(ModuleContext{}); err != nil {
		t.Fatal(err)
	}

	result := module.runMessagingNormalize([]byte(`{
  "platform": "telegram",
  "payload": {
    "update_id": 10,
    "message": {
      "message_id": 99,
      "date": 1700000000,
      "text": "/roll 1d100",
      "chat": {"id": -1001, "type": "group", "title": "CoC"},
      "from": {"id": 42, "username": "keeper", "first_name": "Arc", "last_name": "Keeper"}
    }
  }
}`))
	var msg unifiedMessage
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		t.Fatalf("normalize result is not json: %s", result)
	}
	if msg.Platform != "telegram" || msg.ChatID != "-1001" || msg.SenderID != "42" || msg.SenderName != "Arc Keeper" || msg.Text != "/roll 1d100" {
		t.Fatalf("unexpected unified message: %+v", msg)
	}
}

func TestMessagingBuildOutboundPayloads(t *testing.T) {
	module := &messagingModule{}
	if err := module.Init(ModuleContext{}); err != nil {
		t.Fatal(err)
	}

	telegram := module.runMessagingBuildOutbound([]byte(`{"platform":"telegram","chat_id":"123","text":"hello"}`))
	if !strings.Contains(telegram, `"chat_id":"123"`) || !strings.Contains(telegram, `"text":"hello"`) {
		t.Fatalf("unexpected telegram outbound: %s", telegram)
	}

	feishu := module.runMessagingBuildOutbound([]byte(`{"platform":"feishu","chat_id":"oc_1","text":"hello"}`))
	if !strings.Contains(feishu, `"receive_id":"oc_1"`) || !strings.Contains(feishu, `"msg_type":"text"`) || !strings.Contains(feishu, `\"text\":\"hello\"`) {
		t.Fatalf("unexpected feishu outbound: %s", feishu)
	}
}

func TestMessagingModuleRegisteredAndCanBeDisabled(t *testing.T) {
	rt := newAgentRuntime(testConfig(t.TempDir()), nil, nil)
	if !hasString(toolNames(rt.buildTools()), "messaging_normalize") {
		t.Fatalf("expected messaging tools in main tool list")
	}
	if _, ok := rt.modules.runtimeSnapshots()["messaging"]; !ok {
		t.Fatalf("expected messaging snapshot")
	}

	config := testConfig(t.TempDir())
	config.DisabledModules = map[string]bool{"messaging": true}
	disabled := newAgentRuntime(config, nil, nil)
	if hasString(toolNames(disabled.buildTools()), "messaging_normalize") {
		t.Fatalf("disabled messaging module should remove tools")
	}
}
