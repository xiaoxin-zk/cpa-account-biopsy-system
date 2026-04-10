package accounthealth

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/bootstrap-state", a.handleBootstrapState)
	mux.HandleFunc("/api/report", a.handleReport)
	mux.HandleFunc("/api/run", a.handleRun)
	mux.HandleFunc("/api/auth/action", a.handleAuthAction)
	mux.HandleFunc("/api/settings/password", a.handleChangePassword)
	mux.HandleFunc("/healthz", a.handleHealthz)
	return mux
}

func (a *App) accessToken() string {
	return strings.TrimSpace(a.webToken)
}

func (a *App) authorize(w http.ResponseWriter, r *http.Request) bool {
	secret := a.accessToken()
	if secret == "" {
		return true
	}
	token := strings.TrimSpace(r.Header.Get("Authorization"))
	if token != "" {
		parts := strings.SplitN(token, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			token = parts[1]
		}
	}
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if token != secret {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
		return false
	}
	return true
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (a *App) handleReport(w http.ResponseWriter, r *http.Request) {
	if !a.hasWebToken() {
		w.WriteHeader(http.StatusPreconditionRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "dashboard password not initialized"})
		return
	}
	if !a.authorize(w, r) {
		return
	}
	a.ensureFreshReport(r.Context())
	a.mu.RLock()
	report := a.lastReport
	lastRunAt := a.lastRunAt
	lastErr := a.lastErr
	lastProbe := a.lastProbe
	a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"report":      report,
		"last_run_at": lastRunAt,
		"last_error":  lastErr,
		"last_probe":  lastProbe,
	})
}

func (a *App) handleRun(w http.ResponseWriter, r *http.Request) {
	if !a.hasWebToken() {
		w.WriteHeader(http.StatusPreconditionRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "dashboard password not initialized"})
		return
	}
	if !a.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	log.Printf("account-health manual probe requested remote=%s", r.RemoteAddr)
	report := a.refresh(r.Context(), true)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "report": report})
}

func (a *App) handleAuthAction(w http.ResponseWriter, r *http.Request) {
	if !a.hasWebToken() {
		w.WriteHeader(http.StatusPreconditionRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "dashboard password not initialized"})
		return
	}
	if !a.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		FileName  string   `json:"file_name"`
		FileNames []string `json:"file_names"`
		Action    string   `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid body"})
		return
	}
	body.FileName = strings.TrimSpace(body.FileName)
	body.Action = strings.TrimSpace(strings.ToLower(body.Action))
	files := make([]string, 0, len(body.FileNames)+1)
	if body.FileName != "" {
		files = append(files, body.FileName)
	}
	for _, fileName := range body.FileNames {
		fileName = strings.TrimSpace(fileName)
		if fileName != "" {
			files = append(files, fileName)
		}
	}
	if len(files) == 0 || body.Action == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "file_name or file_names and action are required"})
		return
	}
	if body.Action != "enable" && body.Action != "disable" && body.Action != "delete" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "unsupported action"})
		return
	}
	failed := make(map[string]string)
	for _, fileName := range files {
		var err error
		switch body.Action {
		case "enable":
			err = a.setAuthDisabled(fileName, false, "manual enable from web")
		case "disable":
			err = a.setAuthDisabled(fileName, true, "manual disable from web")
		case "delete":
			err = a.deleteAuth(fileName)
		}
		if err != nil {
			failed[fileName] = err.Error()
		}
	}
	if len(failed) > 0 {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "partial failure", "failed": failed})
		return
	}
	report := a.refresh(r.Context(), false)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "report": report})
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "time": time.Now().UTC()})
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if a.hasWebToken() && !a.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid body"})
		return
	}
	body.Password = strings.TrimSpace(body.Password)
	if len(body.Password) < 6 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "password must be at least 6 characters"})
		return
	}
	if err := a.updateWebToken(body.Password); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "force_relogin": true})
}

func (a *App) handleBootstrapState(w http.ResponseWriter, r *http.Request) {
	a.ensureFreshReport(r.Context())
	a.mu.RLock()
	authCount := len(a.lastReport.Auths)
	lastErr := a.lastErr
	lastRunAt := a.lastRunAt
	a.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"initialized": a.hasWebToken(),
		"auth_count":  authCount,
		"last_error":  lastErr,
		"last_run_at": lastRunAt,
	})
}

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>CPA账号活检系统</title>
  <style>
    :root { color-scheme: dark; --bg:#0b1020; --card:#121933; --muted:#8ea0c9; --text:#edf2ff; --ok:#22c55e; --warn:#f59e0b; --bad:#ef4444; --line:#273252; }
    * { box-sizing: border-box; }
    body { margin:0; font-family: ui-sans-serif, system-ui, sans-serif; background: linear-gradient(180deg,#0a0f1d,#111936 60%,#0a1022); color:var(--text); }
    .wrap { max-width: 1400px; margin: 0 auto; padding: 24px; }
    .top { display:flex; flex-wrap:wrap; gap:12px; align-items:center; justify-content:space-between; margin-bottom:18px; }
    .title { font-size: 28px; font-weight:700; }
    .muted { color: var(--muted); }
    .brand-meta { margin-top:8px; font-size:12px; color:var(--muted); display:flex; gap:10px; flex-wrap:wrap; }
    .grid { display:grid; grid-template-columns: 1.1fr .9fr; gap:14px; margin-bottom:16px; align-items:start; }
    .card { background: rgba(18,25,51,.88); border:1px solid var(--line); border-radius:16px; padding:16px; backdrop-filter: blur(12px); }
    .num { font-size: 28px; font-weight: 700; margin-top: 8px; }
    .bar { display:flex; gap:10px; flex-wrap:wrap; margin-bottom: 14px; }
    .toolbar { display:flex; gap:10px; flex-wrap:wrap; align-items:center; margin: 0 0 14px; }
    .toolbar-group { display:flex; gap:8px; flex-wrap:wrap; align-items:center; }
    button { background:#5b7cff; color:#fff; border:none; border-radius:10px; padding:10px 14px; cursor:pointer; font-weight:600; }
    button.danger { background:#8b1e35; }
    button.warn { background:#8a5a0a; }
    button.ghost { background:#19233f; }
    input { background:#0b1226; color:#fff; border:1px solid var(--line); border-radius:10px; padding:10px 12px; min-width: 260px; }
    table { width:100%; border-collapse: collapse; font-size:14px; }
    th,td { border-bottom:1px solid var(--line); padding:10px 8px; text-align:left; vertical-align: top; }
    th { color:#b9c7ea; font-weight:600; position: sticky; top: 0; background:#121933; }
    .tag { display:inline-block; padding:2px 8px; border-radius:999px; font-size:12px; font-weight:700; }
    .meter { margin-top:8px; height:8px; width:100%; background:#0b1226; border:1px solid var(--line); border-radius:999px; overflow:hidden; }
    .meter > span { display:block; height:100%; border-radius:999px; }
    .active { background:rgba(34,197,94,.16); color:#86efac; }
    .quota { background:rgba(245,158,11,.16); color:#fcd34d; }
    .blocked,.error,.disabled { background:rgba(239,68,68,.16); color:#fca5a5; }
    .cooling { background:rgba(96,165,250,.16); color:#93c5fd; }
    .unprobed { background:rgba(148,163,184,.16); color:#cbd5e1; }
    .small { font-size:12px; color:var(--muted); }
    .table { overflow:auto; max-height: calc(100vh - 260px); border:1px solid var(--line); border-radius:16px; }
    .login { max-width:420px; margin: 10vh auto; }
    .hidden { display:none; }
    .helper { margin-top:12px; font-size:12px; color:var(--muted); }
    .flash-updated { animation: flashUpdated 4s ease-out; }
    .summary-box { margin-top:8px; padding:8px 10px; border:1px solid var(--line); border-radius:12px; background:#0d152d; font-size:12px; color:#cbd5e1; }
    @keyframes flashUpdated { 0%% { background: rgba(34,197,94,.22); } 100%% { background: transparent; } }
    .ring-wrap { display:flex; align-items:center; gap:14px; }
    .ring { width:88px; height:88px; border-radius:50%; position:relative; flex:0 0 88px; }
    .ring::after { content:''; position:absolute; inset:10px; background:#121933; border-radius:50%; border:1px solid var(--line); }
    .ring-value { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; font-weight:700; z-index:1; }
    .legend { display:flex; gap:10px; flex-wrap:wrap; }
    .legend span { font-size:12px; color:var(--muted); }
    .quota-box { margin-top:8px; border:1px solid var(--line); border-radius:12px; padding:8px; background:#0d152d; }
    .quota-head { display:flex; justify-content:space-between; align-items:center; gap:8px; margin-bottom:6px; }
    .quota-value { font-weight:700; }
    .quota-list { display:flex; flex-direction:column; gap:6px; margin-top:8px; }
    .quota-mini { display:flex; flex-direction:column; gap:4px; padding:6px 8px; border:1px solid var(--line); border-radius:10px; background:#0d152d; }
    .quota-mini-top { display:flex; justify-content:space-between; gap:8px; align-items:center; }
    .quota-mini .label { font-size:12px; color:var(--muted); }
    .quota-mini .value { font-size:12px; color:#e2e8f0; font-weight:700; white-space:nowrap; }
    .pill-row { display:flex; gap:6px; flex-wrap:wrap; margin-top:6px; }
    .pill { display:inline-flex; align-items:center; padding:2px 8px; border-radius:999px; font-size:12px; border:1px solid var(--line); color:#cbd5e1; background:#0d152d; }
    .provider-pill { font-weight:700; }
    .switch-pill { font-weight:700; }
    .switch-enabled { color:#86efac; border-color:#22c55e; background:rgba(34,197,94,.14); }
    .switch-disabled { color:#fca5a5; border-color:#ef4444; background:rgba(239,68,68,.14); }
    .provider-codex { color:#93c5fd; border-color:#3b82f6; background:rgba(59,130,246,.14); }
    .provider-claude { color:#fcd34d; border-color:#f59e0b; background:rgba(245,158,11,.14); }
    .provider-gemini { color:#86efac; border-color:#22c55e; background:rgba(34,197,94,.14); }
    .provider-qwen,.provider-kimi,.provider-iflow { color:#c4b5fd; border-color:#8b5cf6; background:rgba(139,92,246,.14); }
    .action-row { display:flex; gap:6px; flex-wrap:wrap; }
    .stat-good { color:#86efac; font-weight:700; }
    .stat-bad { color:#fca5a5; font-weight:700; }
    .quota-stack { display:flex; flex-direction:column; gap:6px; min-width:260px; }
    .quota-empty { padding:6px 8px; border:1px dashed var(--line); border-radius:10px; color:var(--muted); font-size:12px; }
    .quota-summary-card { grid-column: span 2; }
    .quota-summary-list { display:flex; flex-direction:column; gap:10px; margin-top:10px; }
    .quota-summary-item { border:1px solid var(--line); border-radius:12px; padding:10px; background:#0d152d; }
    .quota-summary-top { display:flex; justify-content:space-between; align-items:baseline; gap:10px; }
    .quota-summary-title { font-size:13px; font-weight:700; color:#e2e8f0; }
    .quota-summary-value { font-size:18px; color:#f8fafc; font-weight:800; white-space:nowrap; }
    .panel-card { padding:18px; background:rgba(15,22,42,.94); }
    .panel-head { display:flex; justify-content:space-between; align-items:flex-start; gap:12px; margin-bottom:14px; }
    .panel-title { font-size:15px; font-weight:800; color:#f8fafc; }
    .panel-subtitle { margin-top:4px; font-size:12px; color:var(--muted); }
    .overview-layout { display:grid; grid-template-columns: minmax(220px, 280px) minmax(0,1fr); gap:14px; align-items:stretch; }
    .overview-hero { border:1px solid var(--line); border-radius:14px; padding:14px; background:#0d152d; display:flex; flex-direction:column; gap:12px; }
    .overview-main { display:grid; grid-template-columns: repeat(5,minmax(0,1fr)); gap:10px; }
    .overview-secondary { display:grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap:10px; margin-top:10px; }
    .metric-tile { border:1px solid var(--line); border-radius:14px; padding:12px; background:#0d152d; min-width:0; }
    .metric-label { font-size:12px; color:var(--muted); }
    .metric-value { margin-top:8px; font-size:24px; font-weight:800; color:#f8fafc; line-height:1; }
    .metric-emphasis .metric-value { color:#86efac; }
    .metric-warn .metric-value { color:#fcd34d; }
    .metric-bad .metric-value { color:#fca5a5; }
    .metric-neutral .metric-value { color:#cbd5e1; }
    .hero-summary { display:flex; flex-direction:column; gap:8px; }
    .hero-line { display:flex; justify-content:space-between; gap:10px; font-size:12px; color:#cbd5e1; }
    .hero-status-row { display:flex; flex-wrap:wrap; gap:8px; }
    .hero-chip { display:inline-flex; align-items:center; gap:6px; padding:4px 8px; border-radius:999px; border:1px solid var(--line); font-size:12px; color:#cbd5e1; background:#121933; }
    .hero-chip strong { color:#f8fafc; }
    .quota-meta-grid { display:grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap:8px; margin-top:8px; }
    .quota-meta-item { padding:8px 10px; border:1px solid var(--line); border-radius:10px; background:#121933; }
    .quota-meta-label { font-size:11px; color:var(--muted); }
    .quota-meta-value { margin-top:4px; font-size:12px; color:#dbe4ff; font-weight:700; }
    .quota-meta-item.subtle .quota-meta-value { color:#aebad8; font-weight:600; }
    .quota-summary-item.tight { border-color:rgba(239,68,68,.34); }
    .quota-footnote { margin-top:12px; padding-top:10px; border-top:1px solid var(--line); font-size:12px; color:var(--muted); }
    .summary-toggle { display:inline-flex; align-items:center; gap:6px; border:1px solid var(--line); background:#121933; color:#dbe4ff; border-radius:999px; padding:6px 10px; font-size:12px; font-weight:700; cursor:pointer; }
    .summary-toggle:hover { background:#17213c; }
    .summary-arrow { display:inline-block; transition: transform .16s ease; }
    .summary-arrow.open { transform: rotate(180deg); }
    .summary-compact { display:grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap:10px; margin-top:10px; }
    .summary-compact-item { border:1px solid var(--line); border-radius:12px; background:#0d152d; padding:10px 12px; }
    .summary-compact-value { margin-top:6px; font-size:14px; font-weight:800; color:#f8fafc; }
    .summary-compact-value.warn { color:#fcd34d; }
    @media (max-width: 1100px) { .grid { grid-template-columns: 1fr; } .overview-layout { grid-template-columns: 1fr; } .overview-main { grid-template-columns: repeat(3,minmax(0,1fr)); } }
    @media (max-width: 980px) { .quota-summary-card { grid-column: span 1; } }
    @media (max-width: 680px) { .grid { grid-template-columns: 1fr; } .overview-main { grid-template-columns: repeat(2,minmax(0,1fr)); } .overview-secondary, .quota-meta-grid, .summary-compact { grid-template-columns: 1fr; } .wrap { padding: 14px; } .title { font-size:22px; } .metric-value { font-size:20px; } .quota-summary-value { font-size:16px; } }
  </style>
</head>
<body>
  <div class="wrap login" id="loginBox">
    <div class="card">
      <div class="title">CPA账号活检系统</div>
      <div class="brand-meta"><span>开发者 Xiaoxin</span><span>版本 0.1-bate</span></div>
      <div class="muted" id="loginDesc" style="margin:10px 0 16px;">请输入活检页面口令。</div>
      <div class="bar">
        <input id="token" type="password" placeholder="请输入页面口令">
        <button id="loginBtn">进入面板</button>
      </div>
      <div class="helper" id="loginHelper">首次安装后，如果未设置密码，页面会引导你先完成初始化。</div>
      <div class="helper" id="systemHelper"></div>
      <div class="small" id="loginMsg"></div>
    </div>
  </div>
  <div class="wrap hidden" id="appBox">
    <div class="top">
      <div>
        <div class="title">CPA账号活检系统</div>
        <div class="brand-meta"><span>开发者 Xiaoxin</span><span>版本 0.1-bate</span></div>
        <div class="muted" id="meta">正在加载...</div>
        <div class="summary-box hidden" id="probeSummary"></div>
      </div>
      <div class="bar">
        <input id="keyword" placeholder="搜索 账号 / 邮箱 / 备注 / 状态 / provider">
        <button id="refresh">刷新快照</button>
        <button id="probeNow" class="ghost">运行探测</button>
        <button id="changePassword" class="ghost">修改密码</button>
        <button id="logout" class="ghost">退出登录</button>
      </div>
    </div>
    <div class="grid" id="cards"></div>
    <div class="toolbar">
      <div class="toolbar-group">
        <button id="pickBlocked" class="danger">选择401封禁</button>
        <button id="pickQuota" class="warn">选择额度耗尽</button>
        <button id="pickActive" class="ghost">选择正常账号</button>
        <button id="pickDisabled" class="ghost">选择已停用</button>
        <button id="clearSelection" class="ghost">清空选择</button>
      </div>
      <div class="toolbar-group">
        <button id="batchEnable" class="ghost">批量启用</button>
        <button id="batchDisable" class="warn">批量停用</button>
        <button id="batchDelete" class="danger">批量删除</button>
      </div>
      <div class="small" id="selectionMeta">已选择 0 个账号</div>
    </div>
    <div class="table">
      <table>
        <thead>
          <tr>
            <th><input id="checkAll" type="checkbox"></th>
            <th>账号</th>
            <th>账号健康 / 额度</th>
            <th>请求统计</th>
            <th>导入时间</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody id="rows"></tbody>
      </table>
    </div>
  </div>
  <script>
    const el = id => document.getElementById(id);
    let current = [];
    let currentReport = {};
    let selected = new Set();
    let quotaSummaryExpanded = false;
    let authToken = localStorage.getItem('account-health-token') || '';
    let bootstrapInitialized = true;
    let recentlyUpdated = new Set();
    function fmtTime(v){ if(!v) return '-'; const d=new Date(v); if(isNaN(d.getTime()) || d.getFullYear() <= 1) return '-'; return d.toLocaleString(); }
    function cls(s){ s=(s||'').toLowerCase(); if(['active','ok','recovered'].includes(s)) return 'active'; if(['quota'].includes(s)) return 'quota'; if(['blocked'].includes(s)) return 'blocked'; if(['cooling'].includes(s)) return 'cooling'; if(['unprobed'].includes(s)) return 'unprobed'; if(['disabled','error'].includes(s)) return s; return 'cooling'; }
    function tag(s){ s=s||'unknown'; const map={active:'正常',ok:'正常',recovered:'已恢复',quota:'额度/限流',blocked:'401封禁',cooling:'冷却中',unprobed:'未探测',disabled:'已停用',error:'异常/不可用',unknown:'未知'}; return '<span class="tag '+cls(s)+'">'+(map[s]||s)+'</span>'; }
    function stateKind(x){
      if(x.probe_current === false) return 'unprobed';
      if(x.effective_state === 'blocked' || x.managed_reason === 'blocked' || x.probe_status === 'blocked') return 'blocked';
      if(x.probe_http_status === 401 && !x.probe_status && x.effective_state !== 'quota') return 'blocked';
      if(x.effective_state === 'quota' || x.managed_reason === 'quota' || x.probe_status === 'quota') return 'quota';
      if(x.effective_state === 'error') return 'error';
      if(x.probe_status === 'error' || x.probe_status === 'unsupported') return 'error';
      if(x.effective_state === 'active') return 'active';
      if(x.disabled) return 'disabled';
      return x.effective_state || 'unknown';
    }
    function overviewStateKind(x){
      if(x.effective_state === 'blocked' || x.managed_reason === 'blocked' || x.probe_status === 'blocked') return 'blocked';
      if(x.probe_http_status === 401 && !x.probe_status && x.effective_state !== 'quota') return 'blocked';
      if(x.effective_state === 'quota' || x.managed_reason === 'quota' || x.probe_status === 'quota') return 'quota';
      if(x.disabled) return 'disabled';
      if(x.effective_state === 'error' || x.probe_status === 'error' || x.probe_status === 'unsupported') return 'error';
      if(x.effective_state === 'active') return 'active';
      if(x.effective_state) return x.effective_state;
      return 'unknown';
    }
    function overviewQuotaEligible(x){
      if(!x) return false;
      if(x.disabled) return false;
      if(x.effective_state !== 'active') return false;
      if(x.managed_reason === 'blocked' || x.managed_reason === 'quota') return false;
      if(x.probe_status === 'blocked' || x.probe_status === 'quota') return false;
      if(Number(x.probe_http_status || 0) === 401) return false;
      return true;
    }
    function overviewCounts(items, summary){
      const total = items.length;
      const blocked = items.filter(x => overviewStateKind(x) === 'blocked').length;
      const quota = items.filter(x => overviewStateKind(x) === 'quota').length;
      const disabled = items.filter(x => x.disabled && overviewStateKind(x) !== 'blocked' && overviewStateKind(x) !== 'quota').length;
      const availableFromSummary = Number(summary && summary.available_accounts || 0);
      const active = availableFromSummary > 0 ? availableFromSummary : items.filter(overviewQuotaEligible).length;
      const requests = items.reduce((sum, x) => sum + (x.proxy_requests || 0), 0);
      const failures = items.reduce((sum, x) => sum + (x.proxy_failures || 0), 0);
      return { total, active, blocked, quota, disabled, requests, failures, abnormal: total - active };
    }
    function meterInfo(x){
      const kind = stateKind(x);
      if(kind === 'blocked') return { label:'额度状态: 401封禁', value:100, color:'#ef4444', exact:false };
      if(kind === 'quota') return { label:'额度状态: 额度/限流异常', value:100, color:'#f59e0b', exact:false };
      if(kind === 'disabled') return { label:'额度状态: 手动/策略停用', value:100, color:'#6b7280', exact:false };
      if(x.subscription_active_until) return { label:'订阅状态', value:100, color:'#22c55e', text:'有效', exact:false };
      if((x.plan_type||'').toLowerCase() === 'free') return { label:'Free', value:100, color:'#60a5fa', text:'100%', exact:false };
      if((x.proxy_requests||0) > 0 || (x.proxy_tokens||0) > 0) return { label:'额度未知: 仅统计代理使用量', value:100, color:'#a78bfa', exact:false };
      return { label:'额度未知: 暂无真实余额接口', value:100, color:'#475569', exact:false };
    }
    function meter(x){
      const info = meterInfo(x);
      return '<div class="quota-box"><div class="quota-head"><div class="small">'+info.label+'</div><div class="quota-value">'+(info.text || '')+'</div></div><div class="meter"><span style="width:'+info.value+'%;background:'+info.color+'"></span></div></div>';
    }
    function authHeaders(){ return authToken ? { Authorization: 'Bearer ' + authToken } : {}; }
    function withNoCache(url){
      const sep = url.indexOf('?') >= 0 ? '&' : '?';
      return url + sep + '_ts=' + Date.now();
    }
    async function fetchJSON(url, options){
      const res = await fetch(withNoCache(url), Object.assign({ cache:'no-store' }, options || {}));
      const data = await res.json();
      return { res, data };
    }
    function showApp(ready){ el('loginBox').classList.toggle('hidden', ready); el('appBox').classList.toggle('hidden', !ready); }
    function renderLoginStatus(data){
      const authCount = Number(data && data.auth_count || 0);
      const lastError = (data && data.last_error || '').trim();
      if(lastError){
        el('loginMsg').textContent = '后端状态异常: ' + lastError;
        return;
      }
      if(authCount > 0){
        el('loginMsg').textContent = '后端已读取 ' + authCount + ' 个账号，登录后即可查看详情。';
        return;
      }
      el('loginMsg').textContent = '后端当前未读取到账号，请检查 auths 目录和安装自检输出。';
    }
    async function loadBootstrapState(){
      const res = await fetch('/api/bootstrap-state');
      const data = await res.json();
      bootstrapInitialized = !!data.initialized;
      const count = Number(data.auth_count || 0);
      const lastErr = (data.last_error || '').trim();
      if(lastErr){
        el('systemHelper').textContent = '当前后端状态异常：' + lastErr;
      } else if(count > 0) {
        el('systemHelper').textContent = '系统已连接主项目，当前已读取 ' + count + ' 个账号。';
      } else {
        el('systemHelper').textContent = '当前尚未读取到账号信息。';
      }
      if(!bootstrapInitialized){
        el('loginBtn').textContent = '初始化密码';
        el('loginDesc').textContent = '首次安装，请先设置仪表台密码。';
        el('loginHelper').textContent = '设置完成后将自动进入面板，并作为后续登录密码使用。';
      } else {
        el('loginBtn').textContent = '进入面板';
        el('loginDesc').textContent = '请输入活检页面口令。';
        el('loginHelper').textContent = '如忘记密码，可在登录后使用“修改密码”。';
      }
      renderLoginStatus(data || {});
    }
    function ringCard(title, segments, text, legend){
      const total = segments.reduce((sum, seg) => sum + seg.value, 0);
      let start = 0;
      const parts = [];
      segments.forEach(seg => {
        const size = total > 0 ? (seg.value / total) * 100 : 0;
        const end = start + size;
        if(size > 0) parts.push(seg.color + ' ' + start + '% ' + end + '%');
        start = end;
      });
      parts.push('#202b49 ' + start + '% 100%');
      return '<div class="overview-hero"><div class="muted">'+title+'</div><div class="ring-wrap"><div class="ring" style="background:conic-gradient('+parts.join(', ')+')"><div class="ring-value">'+text+'</div></div><div class="hero-summary"><div class="hero-status-row">'+legend+'</div></div></div></div>';
    }
    function quotaSummaryVisual(value){
      if(value <= 0) return { color:'#ef4444', text:'0%' };
      if(value <= 33) return { color:'#ef4444', text:value + '%' };
      if(value <= 66) return { color:'#f59e0b', text:value + '%' };
      return { color:'#22c55e', text:value + '%' };
    }
    function quotaSummaryCard(summary){
      const windows = Array.isArray(summary && summary.windows) ? summary.windows : [];
      const available = Number(summary && summary.available_accounts || 0);
      const withQuota = Number(summary && summary.accounts_with_quota || 0);
      const missing = Number(summary && summary.missing_snapshots || 0);
      const helper = ['基于最近一次额度快照汇总'];
      const sortedWindows = windows.slice().sort((a,b) => Number(a.percent || 0) - Number(b.percent || 0));
      const tightWindow = sortedWindows.length ? sortedWindows[0] : null;
      const toggle = '<button class="summary-toggle" onclick="toggleQuotaSummary()"><span>'+(quotaSummaryExpanded ? '收起详情' : '展开查看')+'</span><span class="summary-arrow'+(quotaSummaryExpanded ? ' open' : '')+'">▾</span></button>';
      const compact = '<div class="summary-compact">'
        + '<div class="summary-compact-item"><div class="small">可用账号</div><div class="summary-compact-value">'+available+'</div></div>'
        + '<div class="summary-compact-item"><div class="small">已纳入汇总</div><div class="summary-compact-value">'+withQuota+'</div></div>'
        + '<div class="summary-compact-item"><div class="small">最紧张额度项</div><div class="summary-compact-value'+(tightWindow && Number(tightWindow.percent || 0) <= 33 ? ' warn' : '')+'">'+(tightWindow ? ((tightWindow.title || '额度') + ' · ' + Number(tightWindow.percent || 0) + '%') : '暂无快照')+'</div></div>'
        + '</div>';
      if(!windows.length || windows.every(item => Number(item.total || 0) <= 0)){
        const emptyMeta = '<div class="quota-meta-grid"><div class="quota-meta-item"><div class="quota-meta-label">可用账号</div><div class="quota-meta-value">'+available+'</div></div><div class="quota-meta-item subtle"><div class="quota-meta-label">已纳入汇总</div><div class="quota-meta-value">'+withQuota+'</div></div></div>';
        return '<div class="card panel-card quota-summary-card"><div class="panel-head"><div><div class="panel-title">汇总剩余额度</div><div class="panel-subtitle">基于最近一次额度快照汇总</div></div>'+toggle+'</div>'+compact+(quotaSummaryExpanded ? (emptyMeta+'<div class="quota-empty" style="margin-top:10px;">当前可用账号暂无可汇总额度快照</div><div class="quota-footnote">'+helper.join(' ')+'</div>') : '')+'</div>';
      }
      const tightKey = sortedWindows.length ? sortedWindows[0].key : '';
      const content = windows.map(item => {
        const total = Math.max(0, Number(item.total || 0));
        const remaining = Math.max(0, Number(item.remaining || 0));
        const percent = Math.max(0, Math.min(100, Number(item.percent || 0)));
        const visual = quotaSummaryVisual(percent);
        const meta = [
          '<div class="quota-meta-item"><div class="quota-meta-label">剩余 / 总量</div><div class="quota-meta-value">' + remaining + ' / ' + total + '</div></div>',
          '<div class="quota-meta-item"><div class="quota-meta-label">已统计账号数</div><div class="quota-meta-value">' + Number(item.known_accounts || 0) + '</div></div>'
        ];
        if(Number(item.missing_accounts || 0) > 0) meta.push('<div class="quota-meta-item subtle"><div class="quota-meta-label">缺失快照</div><div class="quota-meta-value">' + Number(item.missing_accounts || 0) + ' 个可用账号暂无该额度快照</div></div>');
        if(item.reset_at) meta.push('<div class="quota-meta-item subtle"><div class="quota-meta-label">最近重置时间</div><div class="quota-meta-value">' + item.reset_at + '</div></div>');
        return '<div class="quota-summary-item'+(item.key === tightKey ? ' tight' : '')+'"><div class="quota-summary-top"><span class="quota-summary-title">'+(item.title || '额度')+'</span><span class="quota-summary-value">'+visual.text+'</span></div><div class="meter"><span style="width:'+percent+'%;background:'+visual.color+'"></span></div><div class="quota-meta-grid">'+meta.join('')+'</div></div>';
      }).join('');
      const summaryMeta = '<div class="quota-meta-grid"><div class="quota-meta-item"><div class="quota-meta-label">可用账号</div><div class="quota-meta-value">'+available+'</div></div><div class="quota-meta-item"><div class="quota-meta-label">已纳入汇总</div><div class="quota-meta-value">'+withQuota+'</div></div>'+(missing > 0 || (summary && summary.has_partial_snapshot) ? '<div class="quota-meta-item subtle"><div class="quota-meta-label">缺失快照提示</div><div class="quota-meta-value">部分账号暂无额度快照</div></div>' : '')+'</div>';
      return '<div class="card panel-card quota-summary-card"><div class="panel-head"><div><div class="panel-title">汇总剩余额度</div><div class="panel-subtitle">基于最近一次额度快照汇总</div></div>'+toggle+'</div>'+compact+(quotaSummaryExpanded ? (summaryMeta+'<div class="quota-summary-list">'+content+'</div><div class="quota-footnote">'+helper.join(' ')+'</div>') : '')+'</div>';
    }
    function toggleQuotaSummary(){
      quotaSummaryExpanded = !quotaSummaryExpanded;
      cards(current);
    }
    window.toggleQuotaSummary = toggleQuotaSummary;
    function metricTile(label, value, clsName){
      return '<div class="metric-tile '+(clsName||'')+'"><div class="metric-label">'+label+'</div><div class="metric-value">'+value+'</div></div>';
    }
    function overviewPanel(items){
      const counts = overviewCounts(items, currentReport.quota_summary || {});
      const total = counts.total;
      const active = counts.active;
      const blocked = counts.blocked;
      const quota = counts.quota;
      const disabled = counts.disabled;
      const requests = counts.requests;
      const failures = counts.failures;
      const abnormal = counts.abnormal;
      const healthLegend = '<span class="hero-chip"><strong>'+active+'</strong> 正常</span><span class="hero-chip"><strong>'+quota+'</strong> 额度</span><span class="hero-chip"><strong>'+blocked+'</strong> 封禁</span>';
      return '<div class="card panel-card">'
        + '<div class="panel-head"><div><div class="panel-title">账号池概览</div><div class="panel-subtitle">快速查看整体健康情况与异常分布</div></div></div>'
        + '<div class="overview-layout">'
        + ringCard('号池健康度', [
            { color:'#22c55e', value:active },
            { color:'#f59e0b', value:quota },
            { color:'#ef4444', value:blocked }
          ], total, healthLegend)
        + '<div>'
        + '<div class="overview-main">'
        + metricTile('总账号', total, 'metric-neutral')
        + metricTile('正常账号', active, 'metric-emphasis')
        + metricTile('额度耗尽', quota, 'metric-warn')
        + metricTile('401封禁', blocked, 'metric-bad')
        + metricTile('已停用', disabled, 'metric-neutral')
        + '</div>'
        + '<div class="overview-secondary">'
        + metricTile('总请求', requests, 'metric-neutral')
        + metricTile('请求失败', failures, failures > 0 ? 'metric-bad' : 'metric-neutral')
        + '</div>'
        + '<div class="helper">当前异常项共 '+abnormal+' 个，优先关注封禁、额度耗尽和已停用账号。</div>'
        + '</div>'
        + '</div>'
        + '</div>';
    }
    function cards(items){
      el('cards').innerHTML = overviewPanel(items) + quotaSummaryCard(currentReport.quota_summary || {});
    }
    function updateSelectionMeta(filtered){
      const all = filtered || current;
      el('selectionMeta').textContent = '已选择 ' + selected.size + ' 个账号';
      el('checkAll').checked = all.length > 0 && all.every(x => selected.has(x.file_name));
    }
    function quotaVisual(value){
      if(value <= 0) return { color:'#64748b', text:'已耗尽' };
      if(value <= 33) return { color:'#ef4444', text:'剩余 ' + value + '%' };
      if(value <= 66) return { color:'#f59e0b', text:'剩余 ' + value + '%' };
      return { color:'#22c55e', text:'剩余 ' + value + '%' };
    }
    function quotaItemBox(item, options){
      if(!item) return '';
      options = options || {};
      const neutral = !!options.neutral;
      const exhausted = !!options.exhausted;
      const stale = item.stale === true;
      const hasPercentField = item.percent !== undefined && item.percent !== null && item.percent !== '';
      const known = item.percent_known === true;
      const hasReset = !!(item.reset_at && String(item.reset_at).trim());
      const value = known ? Math.max(0, Math.min(100, hasPercentField ? Number(item.percent) || 0 : 0)) : (hasReset ? 100 : 0);
      const visual = quotaVisual(value);
      const color = neutral ? '#475569' : (exhausted ? '#64748b' : visual.color);
      const title = item.title || '额度';
      const details = [];
      if(hasReset) details.push('<div class="small">'+item.reset_at+'</div>');
      if(stale) details.push('<div class="small">本次未返回，保留上次额度快照</div>');
      let text = '';
      if (known) {
        text = exhausted && value === 0 ? (hasReset ? '等待重置' : '已耗尽') : visual.text;
      } else if (hasReset) {
        text = exhausted ? '等待重置' : (stale ? '上次快照' : '已采集');
      } else if (stale) {
        text = '上次快照';
      } else if (neutral) {
        text = '未采集';
      } else {
        text = '额度耗尽';
      }
      return '<div class="quota-mini"><div class="quota-mini-top"><span class="label">'+title+'</span><span class="value">'+text+'</span></div>'+(details.length?details.join(''):'')+'<div class="meter"><span style="width:'+value+'%;background:'+color+'"></span></div></div>';
    }
    function fallbackQuotaItems(x){
      const plan = (x.plan_type || '').toLowerCase();
      if(plan === 'free') return [{ title:'周限额' }, { title:'代码审查周限额' }];
      if(plan === 'team' || plan === 'plus') return [{ title:'5小时限额' }, { title:'周限额' }, { title:'代码审查周限额' }];
      return [];
    }
    function quotaReasonBox(text){
      return '<div class="quota-empty">'+text+'</div>';
    }
    function quotaBoxes(x){
      const kind = stateKind(x);
      if(Array.isArray(x.quota_items) && x.quota_items.length > 0){
        const relevant = x.quota_items.filter(item => item && ((item.percent_known === true) || !!(item.reset_at && String(item.reset_at).trim()) || item.stale === true || !!(item.title && String(item.title).trim())));
        const rendered = relevant.map(item => quotaItemBox(item, { neutral:!(item.percent_known === true || !!(item.reset_at && String(item.reset_at).trim())), exhausted:((item.percent_known === true && Number(item.percent || 0) <= 0) || (!(item.percent_known === true) && !!item.reset_at && kind === 'quota')) })).filter(Boolean).join('');
        if(rendered) return '<div class="quota-stack">'+rendered+'</div>';
      }
      if(x.quota_percent_known){
        return quotaItemBox({ title:x.quota_title, percent:x.quota_percent, percent_known:x.quota_percent_known, reset_at:x.quota_reset_at }, { neutral:false, exhausted:(kind === 'quota' && Number(x.quota_percent || 0) <= 0) });
      }
      if(kind === 'quota') {
	        const fallback = fallbackQuotaItems(x);
	        if(fallback.length) return '<div class="quota-stack">'+fallback.map(item => quotaItemBox({ title:item.title, reset_at:x.quota_reset_at }, { neutral:false, exhausted:true })).join('')+'</div>';
	        return quotaReasonBox('额度受限，等待重置');
      }
      if(kind === 'unprobed') {
	        return quotaReasonBox('未探测');
      }
      if(kind === 'blocked') {
	        return quotaReasonBox('401封禁，未返回额度');
      }
	      if(kind === 'error') {
	        if(x.probe_http_status === 402 || String(x.probe_message || '').includes('deactivated_workspace')) return quotaReasonBox('工作区不可用（402），当前不可正常使用');
	        return quotaReasonBox('探测异常，当前状态不可确认');
	      }
	      if(!x.probe_status) return quotaReasonBox('未探测');
	      if(x.probe_status === 'ok') return quotaReasonBox('已探测，未返回额度');
	      if(x.probe_status === 'error' || x.probe_status === 'unsupported') return quotaReasonBox('探测失败');
	      return quotaReasonBox('无额度数据');
    }
    function rows(items){
      const kw = el('keyword').value.trim().toLowerCase();
      const filtered = items.filter(x => !kw || JSON.stringify(x).toLowerCase().includes(kw));
      el('rows').innerHTML = filtered.map(x => {
        const checked = selected.has(x.file_name) ? 'checked' : '';
        const rowClass = recentlyUpdated.has(x.file_name) ? ' class="flash-updated"' : '';
        const provider = (x.provider || '').toLowerCase();
        const accountType = (x.provider || '-').toUpperCase();
        const switchState = x.disabled ? '停用' : '启用';
        const switchClass = x.disabled ? 'switch-disabled' : 'switch-enabled';
        const name = '<div class="pill-row"><span class="pill provider-pill provider-'+provider+'">'+accountType+'</span><span class="pill switch-pill '+switchClass+'">'+switchState+'</span></div><strong>'+ (x.email || x.display_name || x.file_name || '-') +'</strong>';
        const success = Math.max(0, (x.proxy_requests||0) - (x.proxy_failures||0));
        const usageParts = ['请求: '+(x.proxy_requests||0), '<span class="stat-good">成功: '+success+'</span> / <span class="stat-bad">失败: '+(x.proxy_failures||0)+'</span>', 'Tokens: '+(x.proxy_tokens||0)];
        if(fmtTime(x.proxy_last_used_at) !== '-') usageParts.push('<span class="small">最后使用: '+fmtTime(x.proxy_last_used_at)+'</span>');
        const usage = usageParts.join('<br>');
        const kind = stateKind(x);
        const stateParts = [tag(kind)];
        const renderedQuotaBoxes = quotaBoxes(x);
        if(renderedQuotaBoxes) stateParts.push(renderedQuotaBoxes);
        const pills = [];
        if((x.plan_type||'').trim()) pills.push('<span class="pill">'+x.plan_type+'</span>');
        if(pills.length) stateParts.push('<div class="pill-row">'+pills.join('')+'</div>');
        const state = stateParts.join('<br>');
        const actions = '<div class="action-row">'+
          '<button class="ghost" onclick="runAction(\''+x.file_name+'\',\'enable\')">启用</button>'+
          '<button class="warn" onclick="runAction(\''+x.file_name+'\',\'disable\')">停用</button>'+
          '<button class="danger" onclick="runAction(\''+x.file_name+'\',\'delete\')">删除</button>'+
          '</div>';
        const importedAt = fmtTime(x.imported_at);
        return '<tr'+rowClass+'><td><input type="checkbox" data-file="'+x.file_name+'" '+checked+' onchange="toggleOne(this)"></td><td>'+name+'</td><td>'+state+'</td><td>'+usage+'</td><td>'+(importedAt === '-' ? '<span class="small">-</span>' : importedAt)+'</td><td>'+actions+'</td></tr>';
      }).join('');
      updateSelectionMeta(filtered);
    }
    function toggleOne(input){
      const fileName = input.getAttribute('data-file');
      if(input.checked) selected.add(fileName); else selected.delete(fileName);
      updateSelectionMeta();
    }
    window.toggleOne = toggleOne;
    function selectBy(predicate){
      const targets = current.filter(predicate).map(x => x.file_name);
      const allSelected = targets.length > 0 && targets.every(name => selected.has(name));
      if(allSelected) targets.forEach(name => selected.delete(name));
      else targets.forEach(name => selected.add(name));
      rows(current);
    }
    async function runAction(fileName, action){
      return runBatchAction(action, [fileName]);
    }
    async function runBatchAction(action, files){
      const textMap = { enable:'启用', disable:'停用', delete:'删除' };
      files = (files || []).filter(Boolean);
      if(!files.length){ alert('请先选择账号'); return; }
      if(!confirm('确认要'+textMap[action]+' '+files.length+' 个账号吗？')) return;
      const { res, data } = await fetchJSON('/api/auth/action', { method:'POST', headers: { 'Content-Type':'application/json', ...authHeaders() }, body: JSON.stringify({ file_names:files, action }) });
      if(!res.ok){ alert(data.error || '操作失败'); return; }
      files.forEach(file => selected.delete(file));
      currentReport = data.report || currentReport;
      current = (data.report && data.report.auths) || current;
      cards(current); rows(current);
    }
    window.runAction = runAction;
    function summarizeProbeChanges(beforeItems, afterItems){
      const beforeMap = new Map((beforeItems || []).map(item => [item.file_name, item]));
      const changed = [];
      let toBlocked = 0, toQuota = 0, toActive = 0;
      for(const item of (afterItems || [])){
        const prev = beforeMap.get(item.file_name);
        const prevKind = prev ? stateKind(prev) : 'unknown';
        const nextKind = stateKind(item);
        const prevKey = prev ? [prevKind, prev.probe_status, prev.managed_reason].join('|') : '';
        const nextKey = [nextKind, item.probe_status, item.managed_reason].join('|');
        if(!prev || prevKey !== nextKey){
          changed.push(item.file_name);
          if(nextKind === 'blocked') toBlocked++;
          if(nextKind === 'quota') toQuota++;
          if(nextKind === 'active' && prevKind !== 'active' && prevKind !== 'unprobed') toActive++;
        }
      }
      return { changed, toBlocked, toQuota, toActive };
    }
    function setProbeSummary(text){
      el('probeSummary').textContent = text;
      el('probeSummary').classList.toggle('hidden', !text);
    }
    async function load(run){
      const before = current.slice();
      el('meta').textContent = run ? '正在执行活检...' : '正在刷新快照...';
      const probeBtn = el('probeNow');
      if(run){ probeBtn.disabled = true; probeBtn.textContent = '活检中...'; setProbeSummary('活检正在执行，请稍候...'); }
      try {
        const initial = run
          ? await fetchJSON('/api/run', { method:'POST', headers: authHeaders() })
          : await fetchJSON('/api/report', { method:'GET', headers: authHeaders() });
        if(initial.res.status === 401){ showApp(false); el('loginMsg').textContent = '口令错误或未登录'; return; }
        let data = initial.data || {};
        if(run){
          const latest = await fetchJSON('/api/report', { method:'GET', headers: authHeaders() });
          if(latest.res.status === 401){ showApp(false); el('loginMsg').textContent = '口令错误或未登录'; return; }
          data = latest.data || data;
        }
        const report = data.report || {};
        currentReport = report;
        current = report.auths || [];
        showApp(true);
        const actionText = run ? '活检完成' : '快照已刷新';
        el('meta').textContent = actionText + ' | 最近运行: ' + fmtTime(data.last_run_at || report.generated_at) + (data.last_error ? ' | 错误: ' + data.last_error : '');
        if(run){
          const summary = summarizeProbeChanges(before, current);
          recentlyUpdated = new Set(summary.changed);
          setProbeSummary('本次活检已完成，更新了 ' + summary.changed.length + ' 个账号状态；其中 401封禁 ' + summary.toBlocked + ' 个，额度受限 ' + summary.toQuota + ' 个，恢复正常 ' + summary.toActive + ' 个。');
          setTimeout(() => { recentlyUpdated = new Set(); rows(current); }, 4000);
        }
        cards(current); rows(current);
      } catch (error) {
        const message = (error && error.message ? error.message : error);
        el('meta').textContent = '操作失败: ' + message;
        if(run) setProbeSummary('活检失败：' + message);
      } finally {
        if(run){ probeBtn.disabled = false; probeBtn.textContent = '运行探测'; }
      }
    }
    async function login(){
      authToken = el('token').value.trim();
      if(!bootstrapInitialized){
        const res = await fetch('/api/settings/password', { method:'POST', headers:{ 'Content-Type':'application/json' }, body: JSON.stringify({ password:authToken }) });
        const data = await res.json();
        if(!res.ok){ el('loginMsg').textContent = data.error || '初始化失败'; return; }
        localStorage.setItem('account-health-token', authToken);
        await loadBootstrapState();
        await load(false);
        return;
      }
      localStorage.setItem('account-health-token', authToken);
      await load(false);
    }
    async function changePassword(){
      const value = prompt('请输入新的仪表台密码（至少6位）');
      if(!value) return;
      const res = await fetch('/api/settings/password', { method:'POST', headers:{ 'Content-Type':'application/json', ...authHeaders() }, body: JSON.stringify({ password:value }) });
      const data = await res.json();
      if(!res.ok){ alert(data.error || '修改失败'); return; }
      localStorage.removeItem('account-health-token');
      authToken = '';
      el('token').value = '';
      showApp(false);
      el('loginMsg').textContent = '密码已修改，请使用新密码重新登录。';
      alert('密码已修改，旧登录态已失效，请重新登录。');
    }
    el('refresh').onclick = () => load(false);
    el('probeNow').onclick = () => load(true);
    el('changePassword').onclick = () => changePassword();
    el('keyword').oninput = () => rows(current);
    el('checkAll').onchange = e => {
      const kw = el('keyword').value.trim().toLowerCase();
      const filtered = current.filter(x => !kw || JSON.stringify(x).toLowerCase().includes(kw));
      if(e.target.checked) filtered.forEach(x => selected.add(x.file_name));
      else filtered.forEach(x => selected.delete(x.file_name));
      rows(current);
    };
    el('pickBlocked').onclick = () => selectBy(x => stateKind(x) === 'blocked');
    el('pickQuota').onclick = () => selectBy(x => stateKind(x) === 'quota');
    el('pickActive').onclick = () => selectBy(x => stateKind(x) === 'active');
    el('pickDisabled').onclick = () => selectBy(x => x.disabled);
    el('clearSelection').onclick = () => { selected.clear(); rows(current); };
    el('batchEnable').onclick = () => runBatchAction('enable', Array.from(selected));
    el('batchDisable').onclick = () => runBatchAction('disable', Array.from(selected));
    el('batchDelete').onclick = () => runBatchAction('delete', Array.from(selected));
    el('loginBtn').onclick = login;
    el('token').addEventListener('keydown', e => { if(e.key === 'Enter') login(); });
    el('logout').onclick = () => { localStorage.removeItem('account-health-token'); authToken=''; showApp(false); };
    loadBootstrapState().then(() => { if(authToken && bootstrapInitialized){ el('token').value = authToken; load(false); } });
    setInterval(() => { if(authToken) load(false); }, 30000);
  </script>
</body>
</html>`
