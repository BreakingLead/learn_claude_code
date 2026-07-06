package agent

import "testing"

func TestStartupConfigNeededRequiresAPIKeyOnly(t *testing.T) {
	if !startupConfigNeeded(RunOptions{}) {
		t.Fatal("expected startup config when api key option is missing")
	}

	if startupConfigNeeded(RunOptions{APIKey: "key"}) {
		t.Fatal("did not expect startup config when api key option is present")
	}
}

func TestStartupConfigApplyToOptions(t *testing.T) {
	options := DefaultRunOptions()
	model := newStartupConfigModel(options)
	model.apiKey.SetValue("key")
	model.model.SetValue("model")
	model.baseURL.SetValue("https://example.test")

	got := model.applyToOptions(options)
	if got.APIKey != "key" || got.Model != "model" || got.FallbackModel != "model" {
		t.Fatalf("unexpected model options: %+v", got)
	}
	if got.BaseURL != "https://example.test" {
		t.Fatalf("unexpected base url: %+v", got)
	}
}
