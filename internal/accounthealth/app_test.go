package accounthealth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestApplyWHAMUsageDetailsMarksQuotaWhenRemainingPercentIsZero(t *testing.T) {
	result := &probeResult{Message: `{"email":"foo@example.com","plan_type":"free","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":100,"reset_after_seconds":100,"reset_at":1776260226},"secondary_window":null}}`}
	applyWHAMUsageDetails(result)
	if !result.QuotaLimited {
		t.Fatal("expected exhausted quota window to mark quota limited even without explicit limit_reached")
	}
	if len(result.QuotaItems) != 1 || !result.QuotaItems[0].PercentKnown || result.QuotaItems[0].Percent != 0 {
		t.Fatalf("expected exhausted weekly window, got %+v", result.QuotaItems)
	}
}

func TestReconcileProbeQuotaWritesRetryAfterChanges(t *testing.T) {
	app := &App{}
	now := time.Now().UTC()
	initialRetry := now.Add(30 * time.Minute).Truncate(time.Second)
	newRetry := now.Add(2 * time.Hour).Truncate(time.Second)
	file := authFile{Disabled: true, Metadata: map[string]any{managedKey: true, managedReasonKey: "quota", managedUntilKey: initialRetry.Format(time.RFC3339)}}
	decision := actionDecision{Disabled: true, Managed: true, ManagedReason: "quota", ManagedRetryAfter: initialRetry, EffectiveState: "quota"}
	updated := app.reconcileProbe(file, decision, probeResult{Status: "quota", ResetAt: newRetry, CheckedAt: now}, now)
	if !updated.Disabled || !updated.Managed || updated.ManagedReason != "quota" {
		t.Fatalf("expected quota-managed decision, got %+v", updated)
	}
	if !sameTime(updated.ManagedRetryAfter, newRetry) {
		t.Fatalf("expected retry_after update to be kept, got %v want %v", updated.ManagedRetryAfter, newRetry)
	}
	if !updated.ShouldWrite {
		t.Fatal("expected changed quota retry_after to trigger file write")
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

func TestMergeProbeIntoSummaryKeepsHistoricalQuotaWindows(t *testing.T) {
	app := &App{}
	summary := &AuthSummary{
		QuotaItems: []QuotaItem{
			{Title: "5小时限额", Percent: 60, PercentKnown: true, ResetAt: "04/10 12:00"},
			{Title: "周限额", Percent: 90, PercentKnown: true, ResetAt: "04/12 12:00"},
			{Title: "代码审查周限额", Percent: 100, PercentKnown: true, ResetAt: "04/13 12:00"},
		},
	}
	app.mergeProbeIntoSummary("foo@example.com", probeResult{
		Status:    "ok",
		CheckedAt: time.Now(),
		QuotaItems: []QuotaItem{
			{Title: "5小时限额", Percent: 58, PercentKnown: true, ResetAt: "04/10 13:00"},
		},
	}, summary)
	if len(summary.QuotaItems) != 3 {
		t.Fatalf("expected historical windows to be preserved, got %+v", summary.QuotaItems)
	}
	if summary.QuotaItems[0].Title != "5小时限额" || summary.QuotaItems[0].Percent != 58 || summary.QuotaItems[0].Stale {
		t.Fatalf("expected fresh primary window, got %+v", summary.QuotaItems[0])
	}
	if summary.QuotaItems[1].Title != "周限额" || !summary.QuotaItems[1].Stale || summary.QuotaItems[1].Percent != 90 {
		t.Fatalf("expected preserved weekly window to remain stale, got %+v", summary.QuotaItems[1])
	}
	if summary.QuotaItems[2].Title != "代码审查周限额" || !summary.QuotaItems[2].Stale {
		t.Fatalf("expected preserved review window to remain stale, got %+v", summary.QuotaItems[2])
	}
}

func TestMergeProbeIntoSummaryRetainsPreviousQuotaSnapshotWhenProbeReturnsNone(t *testing.T) {
	app := &App{}
	summary := &AuthSummary{
		QuotaItems: []QuotaItem{{Title: "周限额", Percent: 14, PercentKnown: true, ResetAt: "04/10 18:00"}},
	}
	app.mergeProbeIntoSummary("foo@example.com", probeResult{Status: "ok", CheckedAt: time.Now()}, summary)
	if len(summary.QuotaItems) != 1 {
		t.Fatalf("expected previous quota snapshot to remain, got %+v", summary.QuotaItems)
	}
	if !summary.QuotaItems[0].Stale {
		t.Fatalf("expected preserved item to be marked stale, got %+v", summary.QuotaItems[0])
	}
	if summary.QuotaPercent != 14 || !summary.QuotaPercentKnown || summary.QuotaResetAt != "04/10 18:00" {
		t.Fatalf("expected summary quota snapshot to remain populated, got %+v", summary)
	}
}

func TestBuildQuotaSummaryCountsOnlyActiveAccounts(t *testing.T) {
	summary := buildQuotaSummary([]AuthSummary{
		{EffectiveState: "active", QuotaItems: []QuotaItem{{Title: "周限额", Percent: 80, PercentKnown: true, ResetAt: "04/10 18:00"}, {Title: "代码审查周限额", Percent: 50, PercentKnown: true}}},
		{EffectiveState: "active", QuotaItems: []QuotaItem{{Title: "周限额", Percent: 40, PercentKnown: true}, {Title: "5小时限额", Percent: 20, PercentKnown: true}}},
		{EffectiveState: "active"},
		{EffectiveState: "quota", Disabled: true, QuotaItems: []QuotaItem{{Title: "周限额", Percent: 0, PercentKnown: true}}},
		{EffectiveState: "blocked", Disabled: true},
		{EffectiveState: "error"},
	}, time.Now())
	if summary.AvailableAccounts != 3 {
		t.Fatalf("expected 3 active accounts, got %+v", summary)
	}
	if summary.AccountsWithQuota != 2 || summary.MissingSnapshots != 1 || !summary.HasPartialSnapshot {
		t.Fatalf("expected partial snapshot accounting, got %+v", summary)
	}
	if len(summary.Windows) != 3 {
		t.Fatalf("expected 3 aggregate windows, got %+v", summary.Windows)
	}
	if summary.Windows[0].Key != "weekly" || summary.Windows[0].Remaining != 120 || summary.Windows[0].Total != 200 || summary.Windows[0].KnownAccounts != 2 {
		t.Fatalf("unexpected weekly aggregate: %+v", summary.Windows[0])
	}
	if summary.Windows[1].Key != "review_weekly" || summary.Windows[1].Remaining != 50 || summary.Windows[1].Total != 100 || summary.Windows[1].KnownAccounts != 1 {
		t.Fatalf("unexpected review aggregate: %+v", summary.Windows[1])
	}
	if summary.Windows[2].Key != "five_hour" || summary.Windows[2].Remaining != 20 || summary.Windows[2].Total != 100 || summary.Windows[2].KnownAccounts != 1 {
		t.Fatalf("unexpected five-hour aggregate: %+v", summary.Windows[2])
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

func TestReconcileProbeMarksDeactivatedWorkspaceAsError(t *testing.T) {
	app := &App{}
	decision := actionDecision{EffectiveState: "active"}
	updated := app.reconcileProbe(authFile{}, decision, probeResult{Status: "error", HTTPStatus: http.StatusPaymentRequired, Message: `{"detail":{"code":"deactivated_workspace"}}`, CheckedAt: time.Now()}, time.Now())
	if updated.EffectiveState != "error" {
		t.Fatalf("expected deactivated workspace to map to error state, got %q", updated.EffectiveState)
	}
	if updated.Disabled {
		t.Fatalf("expected deactivated workspace not to auto-disable account, got %+v", updated)
	}
}

func TestApplyCachedStatusPromotesErrorStateOnReportRefresh(t *testing.T) {
	app := &App{probeCache: map[string]probeCacheEntry{
		"foo@example.com": {Status: "error", HTTPStatus: http.StatusPaymentRequired, Message: `{"detail":{"code":"deactivated_workspace"}}`, CheckedAt: time.Now()},
	}}
	decision := actionDecision{EffectiveState: "active"}
	app.applyCachedStatus("foo@example.com", &decision)
	if decision.EffectiveState != "error" {
		t.Fatalf("expected cached error status to keep report state as error, got %q", decision.EffectiveState)
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

func TestPasswordChangeInvalidatesOldToken(t *testing.T) {
	tempDir := t.TempDir()
	app := &App{authDir: tempDir, webToken: "oldpass1"}
	h := app.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	req.Header.Set("Authorization", "Bearer oldpass1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected old password to work before change, got %d", w.Code)
	}

	changeReq := httptest.NewRequest(http.MethodPost, "/api/settings/password", strings.NewReader(`{"password":"newpass2"}`))
	changeReq.Header.Set("Authorization", "Bearer oldpass1")
	changeReq.Header.Set("Content-Type", "application/json")
	changeW := httptest.NewRecorder()
	h.ServeHTTP(changeW, changeReq)
	if changeW.Code != http.StatusOK {
		t.Fatalf("expected password change success, got %d body=%s", changeW.Code, changeW.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(changeW.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON response: %v", err)
	}
	if force, ok := payload["force_relogin"].(bool); !ok || !force {
		t.Fatalf("expected force_relogin=true, got %#v", payload)
	}

	oldReq := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	oldReq.Header.Set("Authorization", "Bearer oldpass1")
	oldW := httptest.NewRecorder()
	h.ServeHTTP(oldW, oldReq)
	if oldW.Code != http.StatusUnauthorized {
		t.Fatalf("expected old password unauthorized after change, got %d", oldW.Code)
	}

	newReq := httptest.NewRequest(http.MethodGet, "/api/report", nil)
	newReq.Header.Set("Authorization", "Bearer newpass2")
	newW := httptest.NewRecorder()
	h.ServeHTTP(newW, newReq)
	if newW.Code != http.StatusOK {
		t.Fatalf("expected new password to work after change, got %d", newW.Code)
	}

	settingsPath := filepath.Join(tempDir, ".account-health-settings.json")
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings file to exist: %v", err)
	}
	if !strings.Contains(string(raw), "newpass2") {
		t.Fatalf("expected persisted new password, got %s", string(raw))
	}
}

func TestBootstrapStateIncludesAuthCountAndLastError(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "one.json"), []byte(`{"type":"codex","email":"one@example.com"}`), 0o600); err != nil {
		t.Fatalf("expected auth file to be written: %v", err)
	}
	app := &App{authDir: tempDir, webToken: "secret12"}
	app.refresh(context.Background(), false)
	h := app.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap-state", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected bootstrap state success, got %d", w.Code)
	}
	var payload struct {
		Initialized bool      `json:"initialized"`
		AuthCount   int       `json:"auth_count"`
		LastError   string    `json:"last_error"`
		LastRunAt   time.Time `json:"last_run_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON response: %v", err)
	}
	if !payload.Initialized {
		t.Fatal("expected initialized=true")
	}
	if payload.AuthCount != 1 {
		t.Fatalf("expected auth_count=1, got %d", payload.AuthCount)
	}
	if payload.LastError != "" {
		t.Fatalf("expected empty last_error, got %q", payload.LastError)
	}
	if payload.LastRunAt.IsZero() {
		t.Fatal("expected last_run_at to be populated")
	}
}
