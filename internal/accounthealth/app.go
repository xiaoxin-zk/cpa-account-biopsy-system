package accounthealth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	defaultListenAddr       = ":18317"
	defaultSnapshotInterval = 5 * time.Minute
	defaultProbeInterval    = 1 * time.Hour
	defaultReportFreshness  = 10 * time.Second
	defaultWatchDebounce    = 2 * time.Second
	defaultRequestTimeout   = 20 * time.Second
	defaultShutdownTimeout  = 10 * time.Second

	managedKey         = "healthcheck_managed"
	managedReasonKey   = "healthcheck_disabled_reason"
	managedAtKey       = "healthcheck_disabled_at"
	managedUntilKey    = "healthcheck_retry_after"
	lastCheckAtKey     = "healthcheck_checked_at"
	lastCheckMsgKey    = "healthcheck_message"
	lastCheckStatusKey = "healthcheck_status"
	lastHTTPStatusKey  = "healthcheck_http_status"
)

type App struct {
	listenAddr       string
	snapshotInterval time.Duration
	probeInterval    time.Duration
	reportFreshness  time.Duration
	watchDebounce    time.Duration
	requestTimeout   time.Duration
	probeTimeout     time.Duration
	probeConcurrency int
	shutdownTimeout  time.Duration
	authDir          string
	configPath       string
	managementURL    string
	managementKey    string
	webToken         string

	httpClient *http.Client

	mu         sync.RWMutex
	lastReport Report
	lastRunAt  time.Time
	lastErr    string
	lastProbe  bool
	hasProbed  bool
	probeCache map[string]probeCacheEntry
}

type Report struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Auths       []AuthSummary `json:"auths"`
}

type AuthSummary struct {
	ID                     string      `json:"id"`
	FileName               string      `json:"file_name"`
	DisplayName            string      `json:"display_name"`
	Provider               string      `json:"provider"`
	ProviderLabel          string      `json:"provider_label"`
	Email                  string      `json:"email,omitempty"`
	Note                   string      `json:"note,omitempty"`
	AuthIndex              string      `json:"auth_index,omitempty"`
	Disabled               bool        `json:"disabled"`
	Managed                bool        `json:"managed"`
	ManagedReason          string      `json:"managed_reason,omitempty"`
	ManagedRetryAfter      time.Time   `json:"managed_retry_after,omitempty"`
	ProbeCurrent           bool        `json:"probe_current"`
	ProxyRequests          int64       `json:"proxy_requests"`
	ProxyFailures          int64       `json:"proxy_failures"`
	ProxyTokens            int64       `json:"proxy_tokens"`
	ProxyLastUsedAt        time.Time   `json:"proxy_last_used_at,omitempty"`
	RuntimeDisabled        bool        `json:"runtime_disabled"`
	RuntimeUnavailable     bool        `json:"runtime_unavailable"`
	RuntimeUnauthorized    int         `json:"runtime_unauthorized"`
	RuntimeRateLimit       int         `json:"runtime_rate_limit"`
	RuntimeFailures        int         `json:"runtime_failures"`
	RuntimeStatus          string      `json:"runtime_status,omitempty"`
	RuntimeStatusMessage   string      `json:"runtime_status_message,omitempty"`
	RuntimeNextRetryAfter  time.Time   `json:"runtime_next_retry_after,omitempty"`
	ProbeStatus            string      `json:"probe_status,omitempty"`
	ProbeMessage           string      `json:"probe_message,omitempty"`
	ProbeHTTPStatus        int         `json:"probe_http_status,omitempty"`
	CheckedAt              time.Time   `json:"checked_at,omitempty"`
	ImportedAt             time.Time   `json:"imported_at,omitempty"`
	PlanType               string      `json:"plan_type,omitempty"`
	SubscriptionActiveTill string      `json:"subscription_active_until,omitempty"`
	QuotaTitle             string      `json:"quota_title,omitempty"`
	QuotaResetAt           string      `json:"quota_reset_at,omitempty"`
	QuotaPercent           int         `json:"quota_percent,omitempty"`
	QuotaPercentKnown      bool        `json:"quota_percent_known,omitempty"`
	QuotaItems             []QuotaItem `json:"quota_items,omitempty"`
	QuotaHint              string      `json:"quota_hint,omitempty"`
	EffectiveState         string      `json:"effective_state"`
	TopModels              []KVPair    `json:"top_models,omitempty"`
}

type QuotaItem struct {
	Title        string `json:"title,omitempty"`
	Percent      int    `json:"percent,omitempty"`
	PercentKnown bool   `json:"percent_known,omitempty"`
	ResetAt      string `json:"reset_at,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
}

type KVPair struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type runtimeHealth struct {
	ID                      string    `json:"id"`
	FileName                string    `json:"file_name,omitempty"`
	Provider                string    `json:"provider"`
	Email                   string    `json:"email,omitempty"`
	AuthIndex               string    `json:"auth_index,omitempty"`
	Disabled                bool      `json:"disabled"`
	Unavailable             bool      `json:"unavailable"`
	ConsecutiveUnauthorized int       `json:"consecutive_unauthorized"`
	ConsecutiveRateLimit    int       `json:"consecutive_rate_limit"`
	ConsecutiveFailures     int       `json:"consecutive_failures"`
	Status                  string    `json:"status,omitempty"`
	StatusMessage           string    `json:"status_message,omitempty"`
	NextRetryAfter          time.Time `json:"next_retry_after,omitempty"`
}

type usageAggregate struct {
	Requests   int64
	Failures   int64
	Tokens     int64
	LastUsedAt time.Time
	ModelUsage map[string]int64
}

type authFile struct {
	Path       string
	Name       string
	Provider   string
	Email      string
	ID         string
	Disabled   bool
	Note       string
	ImportedAt time.Time
	Metadata   map[string]any
	Raw        []byte
}

type probeResult struct {
	Status       string      `json:"status"`
	Message      string      `json:"message,omitempty"`
	HTTPStatus   int         `json:"http_status,omitempty"`
	CheckedAt    time.Time   `json:"checked_at"`
	PlanType     string      `json:"plan_type,omitempty"`
	ResetAt      time.Time   `json:"reset_at,omitempty"`
	QuotaItems   []QuotaItem `json:"quota_items,omitempty"`
	QuotaLimited bool        `json:"quota_limited,omitempty"`
}

type managementAPICallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       string              `json:"body"`
}

type probeCacheEntry struct {
	Status       string
	Message      string
	HTTPStatus   int
	CheckedAt    time.Time
	PlanType     string
	QuotaItems   []QuotaItem
	QuotaTitle   string
	QuotaPercent int
	QuotaKnown   bool
	QuotaResetAt string
}

func probeCacheKey(file authFile) string {
	if email := strings.ToLower(strings.TrimSpace(file.Email)); email != "" {
		return email
	}
	if id := strings.ToLower(strings.TrimSpace(file.ID)); id != "" {
		return id
	}
	return strings.ToLower(strings.TrimSpace(file.Name))
}

func (a *App) applyProbeCache(key string, summary *AuthSummary) {
	if a == nil || summary == nil || strings.TrimSpace(key) == "" {
		return
	}
	a.mu.RLock()
	entry, ok := a.probeCache[key]
	a.mu.RUnlock()
	if !ok {
		return
	}
	if summary.ProbeStatus == "" {
		summary.ProbeStatus = entry.Status
	}
	if summary.ProbeMessage == "" {
		summary.ProbeMessage = entry.Message
	}
	if summary.ProbeHTTPStatus == 0 {
		summary.ProbeHTTPStatus = entry.HTTPStatus
	}
	summary.CheckedAt = entry.CheckedAt
	if summary.PlanType == "" {
		summary.PlanType = entry.PlanType
	}
	if len(entry.QuotaItems) > 0 {
		summary.QuotaItems = mergeQuotaItems(summary.QuotaItems, entry.QuotaItems, false)
		applyQuotaSnapshot(summary, summary.QuotaItems)
	}
}

func (a *App) applyCachedStatus(key string, result *actionDecision) {
	if a == nil || result == nil || strings.TrimSpace(key) == "" {
		return
	}
	a.mu.RLock()
	entry, ok := a.probeCache[key]
	a.mu.RUnlock()
	if !ok {
		return
	}
	switch entry.Status {
	case "blocked":
		if result.ManagedReason == "blocked" || result.Disabled {
			result.EffectiveState = "blocked"
		}
	case "quota":
		if result.ManagedReason == "quota" || result.Disabled {
			result.EffectiveState = "quota"
			if result.ManagedRetryAfter.IsZero() && entry.CheckedAt.After(time.Time{}) {
				result.ManagedRetryAfter = entry.CheckedAt
			}
		}
	}
}

func (a *App) mergeProbeIntoSummary(key string, probe probeResult, summary *AuthSummary) {
	if summary == nil {
		return
	}
	var previous probeCacheEntry
	if a != nil && strings.TrimSpace(key) != "" {
		a.mu.RLock()
		previous = a.probeCache[key]
		a.mu.RUnlock()
	}
	summary.ProbeStatus = probe.Status
	summary.ProbeMessage = probe.Message
	summary.ProbeHTTPStatus = probe.HTTPStatus
	summary.CheckedAt = probe.CheckedAt
	if summary.PlanType == "" && strings.TrimSpace(probe.PlanType) != "" {
		summary.PlanType = strings.TrimSpace(probe.PlanType)
	}
	quotaItems := mergeQuotaItems(summary.QuotaItems, probe.QuotaItems, true)
	if len(quotaItems) > 0 {
		summary.QuotaItems = quotaItems
		applyQuotaSnapshot(summary, quotaItems)
	}
	if a != nil && strings.TrimSpace(key) != "" {
		entry := probeCacheEntry{
			Status:     probe.Status,
			Message:    probe.Message,
			HTTPStatus: probe.HTTPStatus,
			CheckedAt:  probe.CheckedAt,
			PlanType:   firstNonEmpty(strings.TrimSpace(probe.PlanType), previous.PlanType),
			QuotaItems: quotaItems,
		}
		if len(quotaItems) > 0 {
			entry.QuotaTitle = quotaItems[0].Title
			entry.QuotaPercent = quotaItems[0].Percent
			entry.QuotaKnown = quotaItems[0].PercentKnown
			entry.QuotaResetAt = quotaItems[0].ResetAt
		}
		a.mu.Lock()
		if a.probeCache == nil {
			a.probeCache = make(map[string]probeCacheEntry)
		}
		a.probeCache[key] = entry
		a.mu.Unlock()
	}
}

func applyQuotaSnapshot(summary *AuthSummary, items []QuotaItem) {
	if summary == nil {
		return
	}
	summary.QuotaTitle = ""
	summary.QuotaPercent = 0
	summary.QuotaPercentKnown = false
	summary.QuotaResetAt = ""
	if len(items) == 0 {
		return
	}
	primary := items[0]
	summary.QuotaTitle = primary.Title
	summary.QuotaPercent = primary.Percent
	summary.QuotaPercentKnown = primary.PercentKnown
	summary.QuotaResetAt = primary.ResetAt
}

func mergeQuotaItems(existing []QuotaItem, latest []QuotaItem, markPreservedStale bool) []QuotaItem {
	if len(existing) == 0 && len(latest) == 0 {
		return nil
	}
	if len(latest) == 0 {
		return markQuotaItemsStale(existing, markPreservedStale)
	}
	existingByTitle := make(map[string]QuotaItem, len(existing))
	for _, item := range existing {
		title := normalizeQuotaTitle(item.Title)
		if title == "" {
			continue
		}
		existingByTitle[title] = item
	}
	seen := make(map[string]struct{}, len(latest))
	out := make([]QuotaItem, 0, len(latest)+len(existing))
	for _, item := range latest {
		title := normalizeQuotaTitle(item.Title)
		if title == "" {
			continue
		}
		merged := item
		if prev, ok := existingByTitle[title]; ok {
			if !merged.PercentKnown && prev.PercentKnown {
				merged.Percent = prev.Percent
				merged.PercentKnown = true
				merged.Stale = markPreservedStale
			}
			if strings.TrimSpace(merged.ResetAt) == "" && strings.TrimSpace(prev.ResetAt) != "" {
				merged.ResetAt = prev.ResetAt
				merged.Stale = markPreservedStale || merged.Stale
			}
		}
		out = append(out, merged)
		seen[title] = struct{}{}
	}
	for _, item := range existing {
		title := normalizeQuotaTitle(item.Title)
		if title == "" {
			continue
		}
		if _, ok := seen[title]; ok {
			continue
		}
		if !quotaItemHasDisplayData(item) {
			continue
		}
		item.Stale = markPreservedStale || item.Stale
		out = append(out, item)
	}
	return compactQuotaItems(out)
}

func markQuotaItemsStale(items []QuotaItem, stale bool) []QuotaItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]QuotaItem, 0, len(items))
	for _, item := range items {
		if !quotaItemHasDisplayData(item) && strings.TrimSpace(item.Title) == "" {
			continue
		}
		item.Stale = stale || item.Stale
		out = append(out, item)
	}
	return compactQuotaItems(out)
}

func quotaItemHasDisplayData(item QuotaItem) bool {
	return item.PercentKnown || strings.TrimSpace(item.ResetAt) != ""
}

func normalizeQuotaTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

type runtimeHealthMaps struct {
	ByID        map[string]*runtimeHealth
	ByFile      map[string]*runtimeHealth
	ByAuthIndex map[string]*runtimeHealth
}

func NewAppFromEnv() (*App, error) {
	listenAddr := strings.TrimSpace(os.Getenv("AH_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	authDir := strings.TrimSpace(os.Getenv("AH_AUTH_DIR"))
	if authDir == "" {
		return nil, fmt.Errorf("AH_AUTH_DIR is required")
	}
	configPath := strings.TrimSpace(os.Getenv("AH_CONFIG_PATH"))
	managementURL := strings.TrimRight(strings.TrimSpace(os.Getenv("AH_MANAGEMENT_URL")), "/")
	managementKey := strings.TrimSpace(os.Getenv("AH_MANAGEMENT_KEY"))
	snapshotInterval := parseDurationEnv("AH_SNAPSHOT_INTERVAL", defaultSnapshotInterval)
	probeInterval := parseDurationEnv("AH_PROBE_INTERVAL", defaultProbeInterval)
	reportFreshness := parseDurationEnv("AH_REPORT_FRESHNESS", defaultReportFreshness)
	watchDebounce := parseDurationEnv("AH_WATCH_DEBOUNCE", defaultWatchDebounce)
	requestTimeout := parseDurationEnv("AH_REQUEST_TIMEOUT", defaultRequestTimeout)
	probeTimeout := parseDurationEnv("AH_PROBE_TIMEOUT", 5*time.Second)
	probeConcurrency := parseIntEnv("AH_PROBE_CONCURRENCY", 16)
	shutdownTimeout := parseDurationEnv("AH_SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	webToken := strings.TrimSpace(os.Getenv("AH_WEB_TOKEN"))

	app := &App{
		listenAddr:       listenAddr,
		snapshotInterval: snapshotInterval,
		probeInterval:    probeInterval,
		reportFreshness:  reportFreshness,
		watchDebounce:    watchDebounce,
		requestTimeout:   requestTimeout,
		probeTimeout:     probeTimeout,
		probeConcurrency: probeConcurrency,
		shutdownTimeout:  shutdownTimeout,
		authDir:          authDir,
		configPath:       configPath,
		managementURL:    managementURL,
		managementKey:    managementKey,
		webToken:         webToken,
		httpClient:       &http.Client{Timeout: requestTimeout},
		probeCache:       make(map[string]probeCacheEntry),
	}
	_ = app.loadSettings()
	return app, nil
}

func (a *App) ListenAddr() string             { return a.listenAddr }
func (a *App) ShutdownTimeout() time.Duration { return a.shutdownTimeout }

func (a *App) Run(ctx context.Context) {
	a.refresh(ctx, false)
	go a.watchAuthDir(ctx)
	snapshotTicker := time.NewTicker(a.snapshotInterval)
	defer snapshotTicker.Stop()
	probeTicker := time.NewTicker(a.probeInterval)
	defer probeTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-snapshotTicker.C:
			a.refresh(ctx, false)
		case <-probeTicker.C:
			a.refresh(ctx, true)
		}
	}
}

func (a *App) watchAuthDir(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	if err := watcher.Add(a.authDir); err != nil {
		return
	}
	var timer *time.Timer
	var timerCh <-chan time.Time
	debounce := a.watchDebounce
	if debounce <= 0 {
		debounce = defaultWatchDebounce
	}
	schedule := func() {
		if timer == nil {
			timer = time.NewTimer(debounce)
			timerCh = timer.C
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(debounce)
		timerCh = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if strings.HasSuffix(strings.ToLower(event.Name), ".json") {
				schedule()
			}
		case <-timerCh:
			a.refresh(ctx, false)
			timerCh = nil
		case <-watcher.Errors:
		}
	}
}

func (a *App) ensureFreshReport(ctx context.Context) {
	a.mu.RLock()
	lastRunAt := a.lastRunAt
	a.mu.RUnlock()
	maxAge := a.reportFreshness
	if maxAge <= 0 {
		maxAge = defaultReportFreshness
	}
	if lastRunAt.IsZero() || time.Since(lastRunAt) > maxAge {
		a.refresh(ctx, false)
	}
}

func (a *App) refresh(ctx context.Context, doProbe bool) Report {
	if doProbe {
		log.Printf("account-health probe started")
	}
	files, err := a.loadAuthFiles()
	if err != nil {
		a.setError(err.Error(), doProbe)
		return Report{GeneratedAt: time.Now().UTC()}
	}
	runtimeMaps := a.fetchRuntimeHealth(ctx)
	usageByIndex := a.fetchUsage(ctx)
	report := Report{GeneratedAt: time.Now().UTC(), Auths: make([]AuthSummary, 0, len(files))}
	if !doProbe {
		for _, file := range files {
			rt := runtimeMaps.ByID[file.ID]
			if rt == nil && file.Name != "" {
				rt = runtimeMaps.ByFile[file.Name]
			}
			if rt == nil {
				authIndex := strings.TrimSpace(anyString(file.Metadata["auth_index"]))
				if authIndex != "" {
					rt = runtimeMaps.ByAuthIndex[authIndex]
				}
			}
			report.Auths = append(report.Auths, a.inspectOne(ctx, file, rt, usageByIndex, false))
		}
	} else {
		type job struct {
			idx  int
			file authFile
		}
		type resultItem struct {
			idx     int
			summary AuthSummary
		}
		workers := a.probeConcurrency
		if workers <= 0 {
			workers = 16
		}
		jobs := make(chan job)
		results := make(chan resultItem, len(files))
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					rt := runtimeMaps.ByID[j.file.ID]
					if rt == nil && j.file.Name != "" {
						rt = runtimeMaps.ByFile[j.file.Name]
					}
					if rt == nil {
						authIndex := strings.TrimSpace(anyString(j.file.Metadata["auth_index"]))
						if authIndex != "" {
							rt = runtimeMaps.ByAuthIndex[authIndex]
						}
					}
					probeCtx, cancel := context.WithTimeout(ctx, a.probeTimeout)
					summary := a.inspectOne(probeCtx, j.file, rt, usageByIndex, true)
					cancel()
					results <- resultItem{idx: j.idx, summary: summary}
				}
			}()
		}
		go func() {
			for idx, file := range files {
				jobs <- job{idx: idx, file: file}
			}
			close(jobs)
			wg.Wait()
			close(results)
		}()
		ordered := make([]AuthSummary, len(files))
		processed := 0
		for item := range results {
			ordered[item.idx] = item.summary
			processed++
			log.Printf("account-health probe result email=%s state=%s probe_status=%s managed_reason=%s", item.summary.Email, item.summary.EffectiveState, item.summary.ProbeStatus, item.summary.ManagedReason)
			if processed%25 == 0 || processed == len(files) {
				log.Printf("account-health probe progress %d/%d", processed, len(files))
			}
		}
		report.Auths = append(report.Auths, ordered...)
	}
	sort.Slice(report.Auths, func(i, j int) bool {
		if report.Auths[i].Provider != report.Auths[j].Provider {
			return strings.ToLower(report.Auths[i].Provider) < strings.ToLower(report.Auths[j].Provider)
		}
		return strings.ToLower(report.Auths[i].FileName) < strings.ToLower(report.Auths[j].FileName)
	})
	a.mu.Lock()
	a.lastReport = report
	a.lastRunAt = report.GeneratedAt
	a.lastErr = ""
	a.lastProbe = doProbe
	if doProbe {
		a.hasProbed = true
	}
	a.mu.Unlock()
	if doProbe {
		log.Printf("account-health probe finished accounts=%d", len(report.Auths))
	}
	return report
}

func (a *App) hasCurrentProbe() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.hasProbed
}

func (a *App) inspectOne(ctx context.Context, file authFile, rt *runtimeHealth, usageByIndex map[string]usageAggregate, doProbe bool) AuthSummary {
	now := time.Now().UTC()
	cacheKey := probeCacheKey(file)
	summary := AuthSummary{
		ID:             firstNonEmpty(file.ID, file.Name),
		FileName:       file.Name,
		DisplayName:    authDisplayName(file),
		Provider:       file.Provider,
		ProviderLabel:  providerLabel(file.Provider),
		Email:          file.Email,
		Note:           file.Note,
		Disabled:       file.Disabled,
		Managed:        boolMeta(file.Metadata, managedKey),
		ProbeCurrent:   a.hasCurrentProbe(),
		CheckedAt:      now,
		ImportedAt:     file.ImportedAt,
		PlanType:       stringMeta(file.Metadata, "plan_type"),
		EffectiveState: "active",
	}
	if rt != nil {
		summary.AuthIndex = rt.AuthIndex
		summary.RuntimeDisabled = rt.Disabled
		summary.RuntimeUnavailable = rt.Unavailable
		summary.RuntimeUnauthorized = rt.ConsecutiveUnauthorized
		summary.RuntimeRateLimit = rt.ConsecutiveRateLimit
		summary.RuntimeFailures = rt.ConsecutiveFailures
		summary.RuntimeStatus = rt.Status
		summary.RuntimeStatusMessage = rt.StatusMessage
		summary.RuntimeNextRetryAfter = rt.NextRetryAfter
	}
	if summary.AuthIndex == "" {
		summary.AuthIndex = stringMeta(file.Metadata, "auth_index")
	}
	if summary.AuthIndex != "" {
		if file.Metadata == nil {
			file.Metadata = make(map[string]any)
		}
		if strings.TrimSpace(anyString(file.Metadata["auth_index"])) == "" {
			file.Metadata["auth_index"] = summary.AuthIndex
		}
	}
	if usage, ok := usageByIndex[summary.AuthIndex]; ok {
		summary.ProxyRequests = usage.Requests
		summary.ProxyFailures = usage.Failures
		summary.ProxyTokens = usage.Tokens
		summary.ProxyLastUsedAt = usage.LastUsedAt
		summary.TopModels = topModels(usage.ModelUsage, 5)
	}
	if provider := strings.ToLower(strings.TrimSpace(file.Provider)); provider == "codex" {
		claims := parseCodexClaims(file.Metadata)
		if summary.PlanType == "" {
			summary.PlanType = claims.PlanType
		}
		summary.SubscriptionActiveTill = claims.ActiveUntil
	}
	applyQuotaDisplay(&summary, file.Metadata)
	a.applyProbeCache(cacheKey, &summary)
	summary.QuotaHint = quotaHint(summary)

	result := a.decideAction(file, rt, now)
	if doProbe {
		probe := a.probe(ctx, file)
		summary.ProbeCurrent = true
		a.mergeProbeIntoSummary(cacheKey, probe, &summary)
		result = a.reconcileProbe(file, result, probe, now)
	}
	if err := a.applyAction(file, result); err != nil {
		summary.ProbeStatus = firstNonEmpty(summary.ProbeStatus, "error")
		summary.ProbeMessage = strings.TrimSpace(summary.ProbeMessage + "; persist failed: " + err.Error())
	}
	summary.Disabled = result.Disabled
	summary.Managed = result.Managed
	summary.ManagedReason = result.ManagedReason
	summary.ManagedRetryAfter = result.ManagedRetryAfter
	summary.EffectiveState = result.EffectiveState
	if !summary.ProbeCurrent {
		summary.ProbeStatus = ""
		summary.ProbeMessage = ""
		summary.ProbeHTTPStatus = 0
		if summary.Disabled && !summary.Managed {
			summary.EffectiveState = "disabled"
		} else {
			summary.EffectiveState = "unprobed"
		}
	}
	return summary
}

type actionDecision struct {
	Disabled          bool
	Managed           bool
	ManagedReason     string
	ManagedRetryAfter time.Time
	Message           string
	Status            string
	HTTPStatus        int
	EffectiveState    string
	ShouldWrite       bool
}

func (a *App) decideAction(file authFile, rt *runtimeHealth, now time.Time) actionDecision {
	decision := actionDecision{
		Disabled:       file.Disabled,
		Managed:        boolMeta(file.Metadata, managedKey),
		ManagedReason:  stringMeta(file.Metadata, managedReasonKey),
		EffectiveState: "active",
	}
	if until := timeMeta(file.Metadata, managedUntilKey); !until.IsZero() {
		decision.ManagedRetryAfter = until
	}
	if rt == nil {
		if decision.Disabled {
			switch decision.ManagedReason {
			case "quota":
				decision.EffectiveState = "quota"
			default:
				decision.EffectiveState = "disabled"
			}
		}
		return decision
	}
	if rt.ConsecutiveUnauthorized > 0 {
		decision.Disabled = true
		decision.Managed = true
		decision.ManagedReason = "blocked"
		decision.Status = "blocked"
		decision.HTTPStatus = http.StatusUnauthorized
		decision.Message = "runtime reported unauthorized"
		decision.EffectiveState = "blocked"
		decision.ShouldWrite = !file.Disabled || !decision.Managed
		return decision
	}
	if rt.ConsecutiveRateLimit > 0 || rt.Unavailable {
		decision.Disabled = true
		decision.Managed = true
		decision.ManagedReason = "quota"
		decision.ManagedRetryAfter = rt.NextRetryAfter
		decision.Status = "quota"
		decision.HTTPStatus = http.StatusTooManyRequests
		decision.Message = "runtime reported rate limit or cooldown"
		decision.EffectiveState = "quota"
		decision.ShouldWrite = !file.Disabled || !decision.Managed || !sameTime(decision.ManagedRetryAfter, rt.NextRetryAfter)
		return decision
	}
	if file.Disabled && decision.Managed && runtimeLooksHealthy(rt) {
		decision.Disabled = false
		decision.Managed = false
		decision.ManagedReason = ""
		decision.ManagedRetryAfter = time.Time{}
		decision.Status = "recovered"
		decision.Message = "runtime reported healthy"
		decision.EffectiveState = "active"
		decision.ShouldWrite = true
		return decision
	}
	if file.Disabled && decision.Managed && decision.ManagedReason == "quota" && !decision.ManagedRetryAfter.IsZero() && !decision.ManagedRetryAfter.After(now) {
		decision.Disabled = false
		decision.Managed = false
		decision.ManagedReason = ""
		decision.ManagedRetryAfter = time.Time{}
		decision.Status = "recovered"
		decision.Message = "quota cooldown elapsed"
		decision.EffectiveState = "active"
		decision.ShouldWrite = true
		return decision
	}
	if file.Disabled {
		switch decision.ManagedReason {
		case "quota":
			decision.EffectiveState = "quota"
		default:
			decision.EffectiveState = "disabled"
		}
	}
	return decision
}

func runtimeLooksHealthy(rt *runtimeHealth) bool {
	if rt == nil {
		return false
	}
	if rt.Unavailable || rt.ConsecutiveUnauthorized > 0 || rt.ConsecutiveRateLimit > 0 || rt.ConsecutiveFailures > 0 {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(rt.Status))
	if status == "active" || status == "ok" || status == "healthy" {
		return true
	}
	return !rt.Disabled && !rt.NextRetryAfter.After(time.Now().UTC())
}

func (a *App) reconcileProbe(file authFile, decision actionDecision, probe probeResult, now time.Time) actionDecision {
	decision.Status = firstNonEmpty(probe.Status, decision.Status)
	decision.Message = firstNonEmpty(probe.Message, decision.Message)
	if probe.HTTPStatus != 0 {
		decision.HTTPStatus = probe.HTTPStatus
	}
	switch probe.Status {
	case "ok":
		if decision.EffectiveState == "blocked" || decision.ManagedReason == "blocked" {
			break
		}
		if file.Disabled && boolMeta(file.Metadata, managedKey) {
			decision.Disabled = false
			decision.Managed = false
			decision.ManagedReason = ""
			decision.ManagedRetryAfter = time.Time{}
			decision.ShouldWrite = true
		}
		decision.EffectiveState = "active"
	case "blocked":
		decision.Disabled = true
		decision.Managed = true
		decision.ManagedReason = "blocked"
		decision.ManagedRetryAfter = time.Time{}
		decision.EffectiveState = "blocked"
		decision.ShouldWrite = !file.Disabled || !boolMeta(file.Metadata, managedKey) || stringMeta(file.Metadata, managedReasonKey) != "blocked"
	case "quota":
		decision.Disabled = true
		decision.Managed = true
		decision.ManagedReason = "quota"
		if !probe.ResetAt.IsZero() {
			decision.ManagedRetryAfter = probe.ResetAt.UTC()
		} else if decision.ManagedRetryAfter.IsZero() {
			decision.ManagedRetryAfter = now.Add(30 * time.Minute)
		}
		decision.EffectiveState = "quota"
		decision.ShouldWrite = !file.Disabled || !boolMeta(file.Metadata, managedKey)
	default:
		if decision.Disabled {
			switch decision.ManagedReason {
			case "blocked":
				decision.EffectiveState = "blocked"
			case "quota":
				decision.EffectiveState = "quota"
			default:
				decision.EffectiveState = "disabled"
			}
		}
	}
	return decision
}

func (a *App) applyAction(file authFile, decision actionDecision) error {
	if !decision.ShouldWrite {
		return nil
	}
	updated := file.Raw
	var err error
	updated, err = sjson.SetBytes(updated, "disabled", decision.Disabled)
	if err != nil {
		return err
	}
	if decision.Managed {
		updated, _ = sjson.SetBytes(updated, managedKey, true)
		updated, _ = sjson.SetBytes(updated, managedReasonKey, decision.ManagedReason)
		updated, _ = sjson.SetBytes(updated, managedAtKey, time.Now().UTC().Format(time.RFC3339))
		if !decision.ManagedRetryAfter.IsZero() {
			updated, _ = sjson.SetBytes(updated, managedUntilKey, decision.ManagedRetryAfter.UTC().Format(time.RFC3339))
		} else {
			updated, _ = sjson.DeleteBytes(updated, managedUntilKey)
		}
	} else {
		updated, _ = sjson.DeleteBytes(updated, managedKey)
		updated, _ = sjson.DeleteBytes(updated, managedReasonKey)
		updated, _ = sjson.DeleteBytes(updated, managedAtKey)
		updated, _ = sjson.DeleteBytes(updated, managedUntilKey)
	}
	updated, _ = sjson.SetBytes(updated, lastCheckAtKey, time.Now().UTC().Format(time.RFC3339))
	updated, _ = sjson.SetBytes(updated, lastCheckStatusKey, decision.Status)
	updated, _ = sjson.SetBytes(updated, lastCheckMsgKey, decision.Message)
	if decision.HTTPStatus > 0 {
		updated, _ = sjson.SetBytes(updated, lastHTTPStatusKey, decision.HTTPStatus)
	} else {
		updated, _ = sjson.DeleteBytes(updated, lastHTTPStatusKey)
	}
	return os.WriteFile(file.Path, append(updated, '\n'), 0o600)
}

func (a *App) probe(ctx context.Context, file authFile) probeResult {
	result := probeResult{Status: "unsupported", CheckedAt: time.Now().UTC()}
	provider := strings.ToLower(strings.TrimSpace(file.Provider))
	refreshToken := stringMeta(file.Metadata, "refresh_token")
	accessToken := stringMeta(file.Metadata, "access_token")
	switch provider {
	case "codex":
		if resultFromMgmt, ok := a.probeCodexViaManagementAPICall(ctx, file); ok {
			return resultFromMgmt
		}
		if accessTokenStillValid(accessToken) {
			return probeResult{Status: "ok", Message: "access token still valid", CheckedAt: time.Now().UTC()}
		}
		if refreshToken == "" {
			result.Message = "missing auth_index or refresh_token"
			return result
		}
		result.Status = "error"
		result.Message = "management api-call unavailable and local refresh is not enabled in standalone mode"
		return result
	default:
		result.Message = "provider probe not implemented"
		return result
	}
}

func (a *App) probeCodexViaManagementAPICall(ctx context.Context, file authFile) (probeResult, bool) {
	if a == nil || strings.TrimSpace(a.managementURL) == "" || strings.TrimSpace(a.managementKey) == "" {
		return probeResult{}, false
	}
	authIndex := strings.TrimSpace(anyString(file.Metadata["auth_index"]))
	if authIndex == "" {
		return probeResult{}, false
	}
	body := map[string]any{
		"auth_index": authIndex,
		"method":     http.MethodGet,
		"url":        "https://chatgpt.com/backend-api/wham/usage",
		"header": map[string]string{
			"Authorization": "Bearer $TOKEN$",
			"Accept":        "application/json",
			"User-Agent":    "Mozilla/5.0 CLIProxyAPI-AccountHealth",
			"Origin":        "https://chatgpt.com",
			"Referer":       "https://chatgpt.com/",
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return probeResult{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.managementURL+"/v0/management/api-call", bytes.NewReader(payload))
	if err != nil {
		return probeResult{}, false
	}
	req.Header.Set("Authorization", "Bearer "+a.managementKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return probeResult{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return probeResult{}, false
	}
	var wrapper managementAPICallResponse
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return probeResult{}, false
	}
	result := probeResult{CheckedAt: time.Now().UTC(), HTTPStatus: wrapper.StatusCode, Message: strings.TrimSpace(wrapper.Body)}
	if strings.TrimSpace(wrapper.Body) != "" {
		applyWHAMUsageDetails(&result)
		applyUsageLimitDetails(&result)
	}
	switch {
	case wrapper.StatusCode == http.StatusUnauthorized:
		result.Status = "blocked"
		if result.Message == "" {
			result.Message = "upstream returned 401"
		}
	case wrapper.StatusCode == http.StatusTooManyRequests || strings.Contains(strings.ToLower(wrapper.Body), "usage_limit_reached") || result.QuotaLimited:
		result.Status = "quota"
		if result.HTTPStatus == 0 {
			result.HTTPStatus = http.StatusTooManyRequests
		}
		if result.Message == "" {
			result.Message = "upstream returned quota limit"
		}
	case wrapper.StatusCode >= http.StatusOK && wrapper.StatusCode < http.StatusMultipleChoices:
		result.Status = "ok"
		if result.Message == "" {
			result.Message = "management api-call probe succeeded"
		}
	default:
		result.Status = "error"
	}
	return result, true
}

func classifyProbeError(err error) probeResult {
	result := probeResult{CheckedAt: time.Now().UTC()}
	if err == nil {
		result.Status = "ok"
		result.Message = "probe succeeded"
		return result
	}
	result.Message = strings.TrimSpace(err.Error())
	applyUsageLimitDetails(&result)
	type statusCoder interface{ StatusCode() int }
	var sc statusCoder
	if errors.As(err, &sc) && sc != nil {
		result.HTTPStatus = sc.StatusCode()
	}
	lower := strings.ToLower(result.Message)
	switch {
	case strings.Contains(lower, "usage_limit_reached"):
		result.Status = "quota"
		if result.HTTPStatus == 0 {
			result.HTTPStatus = http.StatusTooManyRequests
		}
	case result.HTTPStatus == http.StatusUnauthorized || strings.Contains(lower, "401") || strings.Contains(lower, "unauthorized"):
		result.Status = "blocked"
		result.HTTPStatus = http.StatusUnauthorized
	case result.HTTPStatus == http.StatusTooManyRequests || strings.Contains(lower, "429") || strings.Contains(lower, "quota") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "capacity"):
		result.Status = "quota"
		if result.HTTPStatus == 0 {
			result.HTTPStatus = http.StatusTooManyRequests
		}
	default:
		result.Status = "error"
	}
	return result
}

func applyUsageLimitDetails(result *probeResult) {
	if result == nil || strings.TrimSpace(result.Message) == "" {
		return
	}
	text := strings.TrimSpace(result.Message)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return
	}
	jsonText := text[start : end+1]
	var payload struct {
		Error struct {
			Type            string `json:"type"`
			Message         string `json:"message"`
			PlanType        string `json:"plan_type"`
			ResetsAt        int64  `json:"resets_at"`
			ResetsInSeconds int64  `json:"resets_in_seconds"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(jsonText), &payload); err != nil {
		return
	}
	if strings.TrimSpace(payload.Error.PlanType) != "" {
		result.PlanType = strings.TrimSpace(payload.Error.PlanType)
	}
	if payload.Error.ResetsAt > 0 {
		result.ResetAt = time.Unix(payload.Error.ResetsAt, 0).UTC()
	} else if payload.Error.ResetsInSeconds > 0 {
		result.ResetAt = time.Now().UTC().Add(time.Duration(payload.Error.ResetsInSeconds) * time.Second)
	}
	if strings.EqualFold(strings.TrimSpace(payload.Error.Type), "usage_limit_reached") {
		items := usageLimitQuotaItems(strings.TrimSpace(payload.Error.PlanType), result.ResetAt)
		if len(items) > 0 {
			result.QuotaItems = items
		}
	}
}

func usageLimitQuotaItems(planType string, resetAt time.Time) []QuotaItem {
	reset := ""
	if !resetAt.IsZero() {
		reset = resetAt.Local().Format("01/02 15:04")
	}
	planType = strings.ToLower(strings.TrimSpace(planType))
	switch planType {
	case "free":
		return []QuotaItem{
			{Title: "周限额", ResetAt: reset},
			{Title: "代码审查周限额", ResetAt: reset},
		}
	case "plus", "team":
		return []QuotaItem{
			{Title: "5小时限额", ResetAt: reset},
			{Title: "周限额", ResetAt: reset},
			{Title: "代码审查周限额", ResetAt: reset},
		}
	default:
		return []QuotaItem{{Title: "额度重置时间", ResetAt: reset}}
	}
}

func applyWHAMUsageDetails(result *probeResult) {
	if result == nil || strings.TrimSpace(result.Message) == "" {
		return
	}
	text := strings.TrimSpace(result.Message)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return
	}
	jsonText := text[start : end+1]
	var payload map[string]any
	if err := json.Unmarshal([]byte(jsonText), &payload); err != nil {
		return
	}
	if plan := firstStringFromMap(payload, "plan_type", "planType"); plan != "" {
		result.PlanType = plan
	}
	planType := strings.ToLower(strings.TrimSpace(result.PlanType))
	items := make([]QuotaItem, 0, 6)
	if planType == "free" {
		items = append(items, parseWHAMLimitItem(payload, "rate_limit", "周限额", "周限额-次窗口", result)...)
		items = append(items, parseWHAMLimitItem(payload, "code_review_rate_limit", "代码审查周限额", "代码审查周限额-次窗口", result)...)
	} else {
		items = append(items, parseWHAMLimitItem(payload, "rate_limit", "5小时限额", "周限额", result)...)
		items = append(items, parseWHAMLimitItem(payload, "code_review_rate_limit", "代码审查5小时限额", "代码审查周限额", result)...)
	}
	if extra, ok := payload["additional_rate_limits"].([]any); ok {
		for _, raw := range extra {
			limit, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title := firstStringFromMap(limit, "display_name", "name", "title")
			if title == "" {
				title = "附加限额"
			}
			items = append(items, parseWHAMLimitWindows(limit, title+"-主窗口", title+"-次窗口", result)...)
		}
	}
	if spend, ok := payload["spend_control"].(map[string]any); ok {
		if reached, ok := spend["reached"].(bool); ok && reached {
			result.QuotaLimited = true
		}
	}
	result.QuotaItems = compactQuotaItems(items)
}

func parseWHAMLimitItem(payload map[string]any, key, primaryTitle, secondaryTitle string, result *probeResult) []QuotaItem {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	limit, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return parseWHAMLimitWindows(limit, primaryTitle, secondaryTitle, result)
}

func parseWHAMLimitWindows(limit map[string]any, primaryTitle, secondaryTitle string, result *probeResult) []QuotaItem {
	if limit == nil {
		return nil
	}
	if reached, ok := limit["limit_reached"].(bool); ok && reached {
		if result != nil {
			result.QuotaLimited = true
		}
	}
	if allowed, ok := limit["allowed"].(bool); ok && !allowed {
		if result != nil {
			result.QuotaLimited = true
		}
	}
	items := make([]QuotaItem, 0, 2)
	if primary, ok := limit["primary_window"].(map[string]any); ok {
		if item := makeWHAMQuotaItem(primary, primaryTitle); item.Title != "" {
			items = append(items, item)
		}
	}
	if secondary, ok := limit["secondary_window"].(map[string]any); ok {
		if item := makeWHAMQuotaItem(secondary, secondaryTitle); item.Title != "" {
			items = append(items, item)
		}
	}
	return items
}

func makeWHAMQuotaItem(window map[string]any, title string) QuotaItem {
	if window == nil {
		return QuotaItem{}
	}
	percent, ok := firstIntFromMap(window, "used_percent", "usedPercent")
	reset := ""
	if unixValue, ok := firstIntFromMap(window, "reset_at", "resetAt"); ok && unixValue > 0 {
		reset = time.Unix(int64(unixValue), 0).Local().Format("01/02 15:04")
	} else if seconds, ok := firstIntFromMap(window, "reset_after_seconds", "resetAfterSeconds"); ok && seconds > 0 {
		reset = time.Now().Add(time.Duration(seconds) * time.Second).Local().Format("01/02 15:04")
	}
	item := QuotaItem{Title: title, ResetAt: reset}
	if ok {
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		item.Percent = 100 - percent
		item.PercentKnown = true
	}
	return item
}

func firstStringFromMap(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			if out := strings.TrimSpace(anyString(value)); out != "" {
				return out
			}
		}
	}
	return ""
}

func firstIntFromMap(data map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if value, ok := data[key]; ok {
			if out, ok := scalarToInt(value); ok {
				return out, true
			}
		}
	}
	return 0, false
}

func compactQuotaItems(items []QuotaItem) []QuotaItem {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]QuotaItem, 0, len(items))
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		item.ResetAt = strings.TrimSpace(item.ResetAt)
		if item.Title == "" {
			continue
		}
		key := strings.ToLower(item.Title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if item.PercentKnown && item.Percent == 0 {
			// keep 0 as a valid remaining value
		}
		if item.Percent < 0 {
			item.Percent = 0
		}
		if item.Percent > 100 {
			item.Percent = 100
		}
		out = append(out, item)
	}
	return out
}

func scalarToInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		v, err := typed.Int64()
		if err == nil {
			return int(v), true
		}
	case string:
		typed = strings.TrimSpace(strings.TrimSuffix(typed, "%"))
		if typed == "" {
			return 0, false
		}
		if parsed, err := strconv.Atoi(typed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func accessTokenStillValid(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return false
	}
	expiresAt := time.Unix(int64(claims.Exp), 0)
	return expiresAt.After(time.Now().Add(2 * time.Minute))
}

func (a *App) loadAuthFiles() ([]authFile, error) {
	files := make([]authFile, 0)
	err := filepath.WalkDir(a.authDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := strings.ToLower(d.Name())
		if d.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".account-health-") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var metadata map[string]any
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil
		}
		provider := strings.TrimSpace(anyString(metadata["type"]))
		if provider == "" {
			provider = strings.TrimSpace(gjson.GetBytes(raw, "type").String())
		}
		file := authFile{
			Path:     path,
			Name:     d.Name(),
			Provider: provider,
			Email:    strings.TrimSpace(anyString(metadata["email"])),
			ID:       firstNonEmpty(strings.TrimSpace(anyString(metadata["id"])), d.Name()),
			Disabled: boolMeta(metadata, "disabled"),
			Note:     strings.TrimSpace(anyString(metadata["note"])),
			Metadata: metadata,
			Raw:      bytes.TrimSpace(raw),
		}
		if info, err := d.Info(); err == nil {
			file.ImportedAt = info.ModTime().UTC()
		}
		files = append(files, file)
		return nil
	})
	return files, err
}

func (a *App) fetchRuntimeHealth(ctx context.Context) runtimeHealthMaps {
	byID := make(map[string]*runtimeHealth)
	byFile := make(map[string]*runtimeHealth)
	byAuthIndex := make(map[string]*runtimeHealth)
	if a.managementURL == "" || a.managementKey == "" {
		return runtimeHealthMaps{ByID: byID, ByFile: byFile, ByAuthIndex: byAuthIndex}
	}
	var payload struct {
		Auths []map[string]any `json:"auths"`
	}
	if err := a.managementGet(ctx, "/v0/management/auth-files/health", &payload); err != nil {
		var fallback struct {
			Files []map[string]any `json:"files"`
		}
		if err2 := a.managementGet(ctx, "/v0/management/auth-files", &fallback); err2 != nil {
			return runtimeHealthMaps{ByID: byID, ByFile: byFile, ByAuthIndex: byAuthIndex}
		}
		payload.Auths = fallback.Files
	}
	for _, item := range payload.Auths {
		rt := &runtimeHealth{
			ID:                      firstNonEmpty(strings.TrimSpace(anyString(item["id"])), strings.TrimSpace(anyString(item["name"]))),
			FileName:                strings.TrimSpace(anyString(item["name"])),
			Provider:                strings.TrimSpace(anyString(item["provider"])),
			Email:                   strings.TrimSpace(anyString(item["email"])),
			AuthIndex:               strings.TrimSpace(anyString(item["auth_index"])),
			Disabled:                anyBool(item["disabled"]),
			Unavailable:             anyBool(item["unavailable"]),
			ConsecutiveUnauthorized: anyInt(item["consecutive_unauthorized"]),
			ConsecutiveRateLimit:    anyInt(item["consecutive_rate_limit"]),
			ConsecutiveFailures:     anyInt(item["consecutive_failures"]),
			Status:                  strings.TrimSpace(anyString(item["status"])),
			StatusMessage:           strings.TrimSpace(anyString(item["status_message"])),
			NextRetryAfter:          parseAnyTime(item["next_retry_after"]),
		}
		if rt.ID != "" {
			byID[rt.ID] = rt
		}
		if rt.FileName != "" {
			byFile[rt.FileName] = rt
		}
		if rt.AuthIndex != "" {
			byAuthIndex[rt.AuthIndex] = rt
		}
	}
	return runtimeHealthMaps{ByID: byID, ByFile: byFile, ByAuthIndex: byAuthIndex}
}

func (a *App) fetchUsage(ctx context.Context) map[string]usageAggregate {
	result := make(map[string]usageAggregate)
	if a.managementURL == "" || a.managementKey == "" {
		return result
	}
	var payload struct {
		Usage struct {
			APIs map[string]struct {
				Models map[string]struct {
					Details []struct {
						Timestamp time.Time `json:"timestamp"`
						AuthIndex string    `json:"auth_index"`
						Failed    bool      `json:"failed"`
						Tokens    struct {
							TotalTokens int64 `json:"total_tokens"`
						} `json:"tokens"`
					} `json:"details"`
				} `json:"models"`
			} `json:"apis"`
		} `json:"usage"`
	}
	if err := a.managementGet(ctx, "/v0/management/usage", &payload); err != nil {
		return result
	}
	for _, api := range payload.Usage.APIs {
		for modelName, model := range api.Models {
			for _, detail := range model.Details {
				idx := strings.TrimSpace(detail.AuthIndex)
				if idx == "" {
					continue
				}
				agg := result[idx]
				agg.Requests++
				if detail.Failed {
					agg.Failures++
				}
				agg.Tokens += detail.Tokens.TotalTokens
				if detail.Timestamp.After(agg.LastUsedAt) {
					agg.LastUsedAt = detail.Timestamp
				}
				if agg.ModelUsage == nil {
					agg.ModelUsage = make(map[string]int64)
				}
				agg.ModelUsage[modelName]++
				result[idx] = agg
			}
		}
	}
	return result
}

func (a *App) managementGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.managementURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.managementKey)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("management %s failed: %d %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *App) setError(msg string, probed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastErr = msg
	a.lastRunAt = time.Now().UTC()
	a.lastProbe = probed
}

func parseDurationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseIntEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func topModels(input map[string]int64, limit int) []KVPair {
	if len(input) == 0 {
		return nil
	}
	out := make([]KVPair, 0, len(input))
	for name, count := range input {
		out = append(out, KVPair{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

type codexClaims struct {
	PlanType    string
	ActiveUntil string
}

func parseCodexClaims(metadata map[string]any) codexClaims {
	idToken := stringMeta(metadata, "id_token")
	if strings.TrimSpace(idToken) == "" {
		return codexClaims{}
	}
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return codexClaims{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return codexClaims{}
	}
	var claims struct {
		OpenAIAuth struct {
			PlanType    string `json:"chatgpt_plan_type"`
			ActiveUntil any    `json:"chatgpt_subscription_active_until"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return codexClaims{}
	}
	return codexClaims{
		PlanType:    strings.TrimSpace(claims.OpenAIAuth.PlanType),
		ActiveUntil: anyString(claims.OpenAIAuth.ActiveUntil),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func authDisplayName(file authFile) string {
	if note := strings.TrimSpace(file.Note); note != "" {
		return note
	}
	if email := strings.TrimSpace(file.Email); email != "" {
		return email
	}
	name := strings.TrimSpace(strings.TrimSuffix(file.Name, filepath.Ext(file.Name)))
	if name != "" {
		return name
	}
	return strings.TrimSpace(file.Name)
}

func providerLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		return "OpenAI / Codex"
	case "claude":
		return "Claude"
	case "gemini", "gemini-cli":
		return "Gemini"
	case "qwen":
		return "Qwen"
	case "kimi":
		return "Kimi"
	case "iflow":
		return "iFlow"
	default:
		if provider == "" {
			return "未知"
		}
		return provider
	}
}

func quotaHint(summary AuthSummary) string {
	if summary.QuotaPercentKnown {
		return ""
	}
	switch summary.EffectiveState {
	case "blocked":
		return "上游返回 401，已视为封禁账号"
	case "quota":
		return "检测到额度/限流/冷却，已自动停用"
	case "active":
		if summary.PlanType != "" || summary.SubscriptionActiveTill != "" {
			return "订阅信息已识别，可结合请求量判断使用情况"
		}
		if summary.ProxyRequests > 0 || summary.ProxyTokens > 0 {
			return "显示的是代理侧累计使用量，不是官方精确余额"
		}
	}
	return "当前未识别到可直接读取的官方剩余额度接口"
}

func applyQuotaDisplay(summary *AuthSummary, metadata map[string]any) {
	if summary == nil {
		return
	}
	items := collectQuotaItems(metadata)
	if len(items) > 0 {
		summary.QuotaItems = items
		summary.QuotaTitle = items[0].Title
		summary.QuotaResetAt = items[0].ResetAt
		summary.QuotaPercent = items[0].Percent
		summary.QuotaPercentKnown = items[0].PercentKnown
	}
}

func collectQuotaItems(metadata map[string]any) []QuotaItem {
	items := make([]QuotaItem, 0, 4)
	seenTitles := make(map[string]struct{})
	weeklyTitle := "Weekly quota"
	reviewTitle := "Code review weekly quota"
	appendItem := func(title string, percent int, known bool, resetAt string) {
		title = strings.TrimSpace(title)
		resetAt = strings.TrimSpace(resetAt)
		if !known && title == "" && resetAt == "" {
			return
		}
		if title != "" {
			if _, exists := seenTitles[title]; exists {
				return
			}
			seenTitles[title] = struct{}{}
		}
		if known {
			if percent < 0 {
				percent = 0
			}
			if percent > 100 {
				percent = 100
			}
		}
		items = append(items, QuotaItem{Title: title, Percent: percent, PercentKnown: known, ResetAt: resetAt})
	}

	if percent, ok := firstIntMeta(metadata, "quota_percent"); ok || stringMeta(metadata, "quota_title") != "" || stringMeta(metadata, "quota_reset_at") != "" {
		appendItem(firstNonEmpty(stringMeta(metadata, "quota_title"), "额度"), percent, ok, stringMeta(metadata, "quota_reset_at"))
	}
	if percent, ok := firstIntMeta(metadata, "weekly_limit_percent", "weekly_percent"); ok || stringMeta(metadata, "weekly_limit_title") != "" || stringMeta(metadata, "weekly_limit_reset_at") != "" {
		appendItem(firstNonEmpty(stringMeta(metadata, "weekly_limit_title"), weeklyTitle), percent, ok, firstNonEmpty(stringMeta(metadata, "weekly_limit_reset_at"), stringMeta(metadata, "weekly_reset_at")))
	}
	if percent, ok := firstIntMeta(metadata, "review_weekly_limit_percent", "code_review_weekly_limit_percent"); ok || stringMeta(metadata, "review_weekly_limit_title") != "" || stringMeta(metadata, "review_weekly_limit_reset_at") != "" {
		appendItem(firstNonEmpty(stringMeta(metadata, "review_weekly_limit_title"), stringMeta(metadata, "code_review_weekly_limit_title"), reviewTitle), percent, ok, firstNonEmpty(stringMeta(metadata, "review_weekly_limit_reset_at"), stringMeta(metadata, "code_review_weekly_limit_reset_at")))
	}
	return items
}

func firstIntMeta(meta map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if meta == nil {
			return 0, false
		}
		value, ok := meta[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case int:
			return typed, true
		case int64:
			return int(typed), true
		case float64:
			return int(typed), true
		case json.Number:
			v, err := typed.Int64()
			if err == nil {
				return int(v), true
			}
		case string:
			typed = strings.TrimSpace(strings.TrimSuffix(typed, "%"))
			if typed == "" {
				continue
			}
			if parsed, err := strconv.Atoi(typed); err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func (a *App) authByFileName(name string) (authFile, error) {
	files, err := a.loadAuthFiles()
	if err != nil {
		return authFile{}, err
	}
	for _, file := range files {
		if strings.EqualFold(file.Name, name) {
			return file, nil
		}
	}
	return authFile{}, os.ErrNotExist
}

func (a *App) setAuthDisabled(name string, disabled bool, reason string) error {
	file, err := a.authByFileName(name)
	if err != nil {
		return err
	}
	updated := file.Raw
	updated, err = sjson.SetBytes(updated, "disabled", disabled)
	if err != nil {
		return err
	}
	if disabled {
		updated, _ = sjson.SetBytes(updated, managedKey, true)
		updated, _ = sjson.SetBytes(updated, managedReasonKey, reason)
		updated, _ = sjson.SetBytes(updated, managedAtKey, time.Now().UTC().Format(time.RFC3339))
	} else {
		updated, _ = sjson.DeleteBytes(updated, managedKey)
		updated, _ = sjson.DeleteBytes(updated, managedReasonKey)
		updated, _ = sjson.DeleteBytes(updated, managedAtKey)
		updated, _ = sjson.DeleteBytes(updated, managedUntilKey)
	}
	updated, _ = sjson.SetBytes(updated, lastCheckAtKey, time.Now().UTC().Format(time.RFC3339))
	updated, _ = sjson.SetBytes(updated, lastCheckStatusKey, map[bool]string{true: "manual_disabled", false: "manual_enabled"}[disabled])
	updated, _ = sjson.SetBytes(updated, lastCheckMsgKey, reason)
	return os.WriteFile(file.Path, append(updated, '\n'), 0o600)
}

func (a *App) deleteAuth(name string) error {
	file, err := a.authByFileName(name)
	if err != nil {
		return err
	}
	return os.Remove(file.Path)
}

func stringMeta(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	return strings.TrimSpace(anyString(meta[key]))
}

func boolMeta(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	return anyBool(meta[key])
}

func timeMeta(meta map[string]any, key string) time.Time {
	if meta == nil {
		return time.Time{}
	}
	return parseAnyTime(meta[key])
}

func parseAnyTime(value any) time.Time {
	s := strings.TrimSpace(anyString(value))
	if s == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func anyBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func anyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		v, _ := typed.Int64()
		return int(v)
	default:
		return 0
	}
}

func sameTime(a, b time.Time) bool {
	if a.IsZero() && b.IsZero() {
		return true
	}
	return a.Equal(b)
}
