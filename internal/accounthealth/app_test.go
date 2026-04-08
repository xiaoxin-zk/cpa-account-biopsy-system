package accounthealth

import (
	"strings"
	"testing"
	"time"
)

func TestCollectQuotaItemsFreeFallback(t *testing.T) {
	items := collectQuotaItems(map[string]any{"plan_type": "free"})
	if len(items) != 0 {
		t.Fatalf("expected no fallback quota items, got %d", len(items))
	}
}

func TestCollectQuotaItemsUsesKnownValues(t *testing.T) {
	items := collectQuotaItems(map[string]any{
		"plan_type":                        "free",
		"weekly_limit_percent":             "62%",
		"weekly_limit_reset_at":            "tomorrow",
		"code_review_weekly_limit_percent": 18,
	})
	if len(items) != 2 {
		t.Fatalf("expected 2 quota items, got %d", len(items))
	}
	if items[0].Title != "Weekly quota" || !items[0].PercentKnown || items[0].Percent != 62 || items[0].ResetAt != "tomorrow" {
		t.Fatalf("unexpected weekly item: %+v", items[0])
	}
	if items[1].Title != "Code review weekly quota" || !items[1].PercentKnown || items[1].Percent != 18 {
		t.Fatalf("unexpected code review item: %+v", items[1])
	}
}

func TestCollectQuotaItemsFreePlanCompletesMissingSlots(t *testing.T) {
	items := collectQuotaItems(map[string]any{
		"plan_type":            "free",
		"quota_title":          "总额度",
		"quota_percent":        80,
		"weekly_limit_percent": 55,
	})
	if len(items) != 2 {
		t.Fatalf("expected 2 quota items, got %d", len(items))
	}
	if items[0].Title != "总额度" || !items[0].PercentKnown || items[0].Percent != 80 {
		t.Fatalf("unexpected generic item: %+v", items[0])
	}
	if items[1].Title != "Weekly quota" || !items[1].PercentKnown || items[1].Percent != 55 {
		t.Fatalf("unexpected weekly item: %+v", items[1])
	}
}

func TestApplyWHAMUsageDetailsParsesQuotaWindows(t *testing.T) {
	result := &probeResult{Message: `{"email":"foo@example.com","plan_type":"free","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":86,"reset_after_seconds":100,"reset_at":1776260226},"secondary_window":null},"code_review_rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"reset_after_seconds":200,"reset_at":1776277523},"secondary_window":null}}`}
	applyWHAMUsageDetails(result)
	if result.PlanType != "free" {
		t.Fatalf("expected free plan, got %q", result.PlanType)
	}
	if len(result.QuotaItems) != 2 {
		t.Fatalf("expected 2 quota items, got %d: %+v", len(result.QuotaItems), result.QuotaItems)
	}
	if result.QuotaLimited {
		t.Fatal("expected non-limited account to remain non-quota")
	}
	if result.QuotaItems[0].Title != "周限额" || !result.QuotaItems[0].PercentKnown || result.QuotaItems[0].Percent != 14 {
		t.Fatalf("unexpected primary quota item: %+v", result.QuotaItems[0])
	}
	if result.QuotaItems[1].Title != "代码审查周限额" || !result.QuotaItems[1].PercentKnown || result.QuotaItems[1].Percent != 100 {
		t.Fatalf("unexpected code review quota item: %+v", result.QuotaItems[1])
	}
	if result.QuotaItems[0].ResetAt == "" || result.QuotaItems[1].ResetAt == "" {
		t.Fatalf("expected reset times to be populated: %+v", result.QuotaItems)
	}
}

func TestApplyWHAMUsageDetailsMarksQuotaWhenLimitReached(t *testing.T) {
	result := &probeResult{Message: `{"email":"foo@example.com","plan_type":"free","rate_limit":{"allowed":false,"limit_reached":true,"primary_window":{"used_percent":100,"reset_after_seconds":100,"reset_at":1776260226},"secondary_window":null}}`}
	applyWHAMUsageDetails(result)
	if !result.QuotaLimited {
		t.Fatal("expected limit_reached to mark quota limited")
	}
	if len(result.QuotaItems) != 1 || !result.QuotaItems[0].PercentKnown || result.QuotaItems[0].Percent != 0 {
		t.Fatalf("expected exhausted weekly window, got %+v", result.QuotaItems)
	}
}

func TestApplyWHAMUsageDetailsUsesTeamWindowTitles(t *testing.T) {
	result := &probeResult{Message: `{"email":"foo@example.com","plan_type":"team","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":40,"reset_after_seconds":100,"reset_at":1776260226},"secondary_window":{"used_percent":10,"reset_after_seconds":200,"reset_at":1776270226}},"code_review_rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":0,"reset_after_seconds":300,"reset_at":1776280226},"secondary_window":null}}`}
	applyWHAMUsageDetails(result)
	if result.QuotaLimited {
		t.Fatal("expected team account with available windows to remain non-limited")
	}
	if len(result.QuotaItems) < 3 {
		t.Fatalf("expected team quota windows, got %+v", result.QuotaItems)
	}
	if result.QuotaItems[0].Title != "5小时限额" || result.QuotaItems[0].Percent != 60 {
		t.Fatalf("unexpected team primary window: %+v", result.QuotaItems[0])
	}
	if result.QuotaItems[1].Title != "周限额" || result.QuotaItems[1].Percent != 90 {
		t.Fatalf("unexpected team secondary window: %+v", result.QuotaItems[1])
	}
}

func TestReconcileProbeBlockedWinsOverOK(t *testing.T) {
	app := &App{}
	decision := actionDecision{Disabled: true, Managed: true, ManagedReason: "blocked", EffectiveState: "blocked"}
	updated := app.reconcileProbe(authFile{Disabled: true, Metadata: map[string]any{managedKey: true, managedReasonKey: "blocked"}}, decision, probeResult{Status: "ok", CheckedAt: time.Now()}, time.Now())
	if updated.EffectiveState != "blocked" {
		t.Fatalf("expected blocked state to win, got %q", updated.EffectiveState)
	}
	if updated.ManagedReason != "blocked" {
		t.Fatalf("expected blocked managed reason to remain, got %q", updated.ManagedReason)
	}
}

func TestClassifyProbeErrorUsageLimitReached(t *testing.T) {
	err := strings.NewReader("")
	_ = err
	result := classifyProbeError(testError(`{"error":{"type":"usage_limit_reached","message":"The usage limit has been reached","plan_type":"free","resets_at":1775926114}}`))
	if result.Status != "quota" {
		t.Fatalf("expected quota status, got %q", result.Status)
	}
	if result.PlanType != "free" {
		t.Fatalf("expected free plan type, got %q", result.PlanType)
	}
	if result.ResetAt.IsZero() {
		t.Fatal("expected reset time to be parsed")
	}
}

type testError string

func (e testError) Error() string { return string(e) }
