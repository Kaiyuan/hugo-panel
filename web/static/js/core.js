(function () {
  'use strict';
  const HP = (window.HP = window.HP || {});
  const { reactive, computed } = Vue;

  const state = reactive({
    ready: false,
    bootError: false,
    setupRequired: false,
    authenticated: false,
    user: null,
    csrf: '',
    version: '',
    languages: [],
    defaults: { language: 'en', theme: 'system' },
    smtpConfigured: false,
    startedAt: '',
    offline: !navigator.onLine,
    installPrompt: null,
    swUpdate: false,
    sessionExpired: false
  });

  const route = reactive({ path: '/dashboard', query: {}, full: '/dashboard' });
  const i18n = reactive({ lang: 'en', messages: {}, loaded: false });
  const theme = reactive({ mode: 'system', resolved: 'light' });

  let toastService = null;
  HP.bindToast = (svc) => { toastService = svc; };
  function toast(severity, summary, detail, life) {
    if (!toastService) return;
    toastService.add({ severity, summary, detail: detail || undefined, life: life || 4000 });
  }

  function t(key, params, fallback) {
    let s = i18n.messages[key];
    if (s === undefined || s === null || s === '') s = fallback !== undefined ? fallback : key;
    if (params && typeof params === 'object') {
      s = String(s).replace(/\{(\w+)\}/g, (m, k) => (params[k] !== undefined ? String(params[k]) : m));
    }
    return s;
  }

  class ApiError extends Error {
    constructor(code, message, status) {
      super(message || code);
      this.code = code || 'server_error';
      this.status = status || 0;
    }
  }
  HP.ApiError = ApiError;

  function sessionExpired() {
    if (state.sessionExpired) return;
    state.sessionExpired = true;
    state.user = null;
    state.authenticated = false;
    state.csrf = '';
    navigate('/login');
    toast('error', t('auth.session_expired_title', {}, 'Session expired'), t('errors.session_expired', {}, 'Please sign in again.'), 6000);
    setTimeout(() => { state.sessionExpired = false; }, 1500);
  }
  HP.sessionExpired = sessionExpired;

  async function api(path, opts) {
    opts = opts || {};
    const method = (opts.method || 'GET').toUpperCase();
    const init = { method, credentials: 'same-origin', headers: {} };
    if (opts.body !== undefined) {
      if (opts.body instanceof FormData) {
        init.body = opts.body;
      } else {
        init.headers['Content-Type'] = 'application/json';
        init.body = JSON.stringify(opts.body);
      }
    }
    if (method !== 'GET' && method !== 'HEAD' && state.csrf) {
      init.headers['X-CSRF-Token'] = state.csrf;
    }
    let res;
    try {
      res = await fetch('/api' + path, init);
    } catch (e) {
      throw new ApiError('network_error', 'Network request failed', 0);
    }
    const ctype = res.headers.get('content-type') || '';
    let data = null;
    if (ctype.indexOf('application/json') >= 0) {
      try { data = await res.json(); } catch (e) { data = null; }
    } else {
      try { data = await res.text(); } catch (e) { data = null; }
    }
    if (res.status === 401 && state.user && path !== '/login' && path !== '/state') {
      sessionExpired();
    }
    if (!res.ok) {
      const code = (data && data.error) || 'server_error';
      const message = (data && data.message) || (typeof data === 'string' ? data : '') || res.statusText;
      throw new ApiError(code, message, res.status);
    }
    return data;
  }
  HP.api = api;

  function errorText(err) {
    if (!err) return t('errors.generic', {}, 'Something went wrong');
    const key = 'errors.' + (err.code || 'generic');
    return t(key, {}, err.message || t('errors.generic', {}, 'Something went wrong'));
  }
  HP.errorText = errorText;
  HP.notifyError = (err) => toast('error', errorText(err), undefined, 6000);

  function detectLanguage() {
    const available = (state.languages || []).map((l) => l.code);
    const candidates = (navigator.languages || [navigator.language || 'en']).map((s) => s.replace('_', '-'));
    for (const c of candidates) {
      const exact = available.find((a) => a.toLowerCase() === c.toLowerCase());
      if (exact) return exact;
    }
    for (const c of candidates) {
      const base = c.split('-')[0].toLowerCase();
      const partial = available.find((a) => a.toLowerCase().split('-')[0] === base);
      if (partial) return partial;
    }
    return null;
  }

  async function loadLocale(code) {
    code = code || 'en';
    if (state.languages && state.languages.length) {
      const match = state.languages.find((l) => l.code === code);
      if (!match) code = state.languages.some((l) => l.code === 'en') ? 'en' : state.languages[0].code;
    }
    try {
      const res = await fetch('/api/i18n/' + encodeURIComponent(code), { credentials: 'same-origin', cache: 'no-store' });
      if (!res.ok) throw new Error('locale request failed');
      const data = await res.json();
      i18n.messages = data.messages || {};
      i18n.lang = code;
      i18n.loaded = true;
      document.documentElement.lang = code;
      try { localStorage.setItem('hp_lang', code); } catch (e) {}
      return true;
    } catch (e) {
      if (code !== 'en' && i18n.lang !== 'en') return loadLocale('en');
      i18n.loaded = true;
      return false;
    }
  }
  HP.loadLocale = loadLocale;

  async function setLocale(code, opts) {
    opts = opts || {};
    const language = (state.languages || []).find((l) => l.code === code);
    if (!language) return false;
    await loadLocale(code);
    if (opts.persistUser !== false && state.user) {
      state.user.language = code;
      try { await api('/me', { method: 'PUT', body: { language: code } }); } catch (e) {}
    }
    return true;
  }
  HP.setLocale = setLocale;

  function applyTheme(mode, opts) {
    opts = opts || {};
    if (mode !== 'light' && mode !== 'dark' && mode !== 'system') mode = 'system';
    theme.mode = mode;
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const resolved = mode === 'system' ? (media.matches ? 'dark' : 'light') : mode;
    theme.resolved = resolved;
    const dark = resolved === 'dark';
    document.documentElement.classList.toggle('dark', dark);
    document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', dark ? '#000000' : '#f6f7f9');
    try { localStorage.setItem('hp_theme', mode); } catch (e) {}
    if (opts.persistUser !== false && state.user) {
      state.user.theme = mode;
      api('/me', { method: 'PUT', body: { theme: mode } }).catch(() => {});
    }
  }
  HP.applyTheme = applyTheme;
  HP.setTheme = (mode) => applyTheme(mode, {});
  HP.toggleTheme = () => applyTheme(theme.resolved === 'dark' ? 'light' : 'dark', {});

  const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
  const onSystemTheme = () => { if (theme.mode === 'system') applyTheme('system', { persistUser: false }); };
  if (mediaQuery.addEventListener) mediaQuery.addEventListener('change', onSystemTheme);
  else if (mediaQuery.addListener) mediaQuery.addListener(onSystemTheme);

  function parseHash() {
    const raw = location.hash.startsWith('#') ? location.hash.slice(1) : '';
    let pathPart = raw;
    let qs = '';
    const qi = raw.indexOf('?');
    if (qi >= 0) { pathPart = raw.slice(0, qi); qs = raw.slice(qi + 1); }
    if (!pathPart || pathPart === '/') {
      pathPart = state.user ? '/dashboard' : '/login';
    }
    if (!pathPart.startsWith('/')) pathPart = '/' + pathPart;
    route.path = pathPart;
    const query = {};
    try {
      const params = new URLSearchParams(qs);
      for (const [k, v] of params.entries()) query[k] = v;
    } catch (e) {}
    route.query = query;
    route.full = pathPart + (qs ? '?' + qs : '');
  }
  function navigate(path, query) {
    let qs = '';
    if (query) {
      const params = new URLSearchParams();
      Object.keys(query).forEach((k) => {
        const v = query[k];
        if (v !== undefined && v !== null && v !== '') params.append(k, v);
      });
      qs = params.toString();
    }
    const target = '#' + path + (qs ? '?' + qs : '');
    if (location.hash === target) parseHash();
    else location.hash = target;
  }
  HP.route = route;
  HP.navigate = navigate;
  HP.parseHash = parseHash;
  window.addEventListener('hashchange', parseHash);

  function applyState(data) {
    if (!data) return;
    state.setupRequired = !!data.setup_required;
    state.authenticated = !!data.authenticated;
    state.user = data.user || null;
    state.csrf = data.csrf || '';
    state.version = data.version || state.version;
    state.languages = data.languages || state.languages;
    state.defaults = data.defaults || state.defaults;
    state.smtpConfigured = !!data.smtp_configured;
    state.startedAt = data.started_at || state.startedAt;
  }
  HP.applyState = applyState;

  async function loadInitial() {
    state.ready = false;
    state.bootError = false;
    try {
      const res = await fetch('/api/state', { credentials: 'same-origin', cache: 'no-store' });
      if (!res.ok) throw new Error('state failed');
      const data = await res.json();
      applyState(data);
      const storedLang = localStorage.getItem('hp_lang');
      const preferred = (state.user && state.user.language) || storedLang || detectLanguage() || state.defaults.language || 'en';
      await loadLocale(preferred);
      const storedTheme = localStorage.getItem('hp_theme');
      const themeMode = (state.user && state.user.theme) || storedTheme || state.defaults.theme || 'system';
      applyTheme(themeMode, { persistUser: false });
      state.ready = true;
      parseHash();
      if (state.user && (route.path === '/login' || route.path === '/forgot')) navigate('/dashboard');
    } catch (e) {
      state.ready = true;
      state.bootError = true;
    }
  }
  HP.loadInitial = loadInitial;

  async function refreshState() {
    const data = await api('/state');
    applyState(data);
    return data;
  }
  HP.refreshState = refreshState;

  async function afterLogin(data) {
    applyState(data);
    if (state.user) {
      await loadLocale(state.user.language || i18n.lang);
      applyTheme(state.user.theme || 'system', { persistUser: false });
    }
    navigate('/dashboard');
  }
  HP.afterLogin = afterLogin;

  async function logout() {
    try { await api('/logout', { method: 'POST' }); } catch (e) {}
    state.user = null;
    state.authenticated = false;
    state.csrf = '';
    navigate('/login');
  }
  HP.logout = logout;

  function updateOnline() {
    const wasOffline = state.offline;
    state.offline = !navigator.onLine;
    if (wasOffline && !state.offline) toast('success', t('common.back_online', {}, 'Back online'));
  }
  window.addEventListener('online', updateOnline);
  window.addEventListener('offline', updateOnline);

  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault();
    state.installPrompt = e;
  });
  HP.installApp = async function () {
    if (!state.installPrompt) return;
    state.installPrompt.prompt();
    try { await state.installPrompt.userChoice; } catch (e) {}
    state.installPrompt = null;
  };

  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js', { updateViaCache: 'none' }).then((reg) => {
        reg.addEventListener('updatefound', () => {
          const worker = reg.installing;
          if (!worker) return;
          worker.addEventListener('statechange', () => {
            if (worker.state === 'installed' && navigator.serviceWorker.controller) state.swUpdate = true;
          });
        });
      }).catch(() => {});
    });
  }

  function formatDate(iso, opts) {
    if (!iso) return '—';
    const d = new Date(iso);
    if (isNaN(d.getTime())) return String(iso);
    const options = opts || { year: 'numeric', month: 'short', day: 'numeric' };
    try { return new Intl.DateTimeFormat(i18n.lang, options).format(d); } catch (e) { return d.toISOString().slice(0, 10); }
  }
  function formatDateTime(iso) {
    return formatDate(iso, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  }
  function formatMonth(month) {
    const parts = String(month).split('-');
    if (parts.length !== 2) return month;
    const d = new Date(Number(parts[0]), Number(parts[1]) - 1, 1);
    try { return new Intl.DateTimeFormat(i18n.lang, { month: 'short' }).format(d); } catch (e) { return month; }
  }
  function formatBytes(n) {
    n = Number(n || 0);
    if (n < 1024) return n + ' B';
    const units = ['KB', 'MB', 'GB', 'TB'];
    let i = -1;
    do { n = n / 1024; i++; } while (n >= 1024 && i < units.length - 1);
    return (Math.round(n * 10) / 10) + ' ' + units[i];
  }
  function formatNumber(n) {
    try { return new Intl.NumberFormat(i18n.lang).format(Number(n || 0)); } catch (e) { return String(n || 0); }
  }
  function initials(name) {
    const s = String(name || '').trim();
    if (!s) return 'HP';
    const parts = s.split(/[\s._-]+/).filter(Boolean);
    return ((parts[0] || s)[0] + (parts.length > 1 ? parts[1][0] : '')).toUpperCase();
  }
  function statusLabel(status) {
    return t('status.' + status, {}, status || '');
  }
  function statusSeverity(status) {
    switch (status) {
      case 'published': return 'success';
      case 'draft': return 'warn';
      case 'scheduled': return 'info';
      case 'error': return 'danger';
      case 'success': return 'success';
      case 'failed': return 'danger';
      case 'running': return 'info';
      default: return 'secondary';
    }
  }
  function slugify(s) {
    const out = String(s || '').normalize('NFKD').replace(/[\u0300-\u036f]/g, '').toLowerCase()
      .replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-+|-+$/g, '').slice(0, 80);
    return out || 'post';
  }
  function toLocalInput(iso) {
    if (!iso) return '';
    const raw = String(iso);
    let d = /^\d{4}-\d{2}-\d{2}$/.test(raw) ? new Date(raw + 'T00:00:00') : new Date(raw);
    if (isNaN(d.getTime())) {
      const m = raw.match(/^(\d{4}-\d{2}-\d{2})$/);
      if (m) d = new Date(m[1] + 'T00:00:00');
    }
    if (isNaN(d.getTime())) return '';
    const pad = (n) => String(n).padStart(2, '0');
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate()) + 'T' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }
  function fromLocalInput(value) {
    if (!value) return '';
    const d = new Date(value);
    if (isNaN(d.getTime())) return value;
    return d.toISOString();
  }
  async function copyText(text) {
    try {
      if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(text);
      else {
        const ta = document.createElement('textarea');
        ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
        document.body.appendChild(ta); ta.select(); document.execCommand('copy'); ta.remove();
      }
      toast('success', t('common.copied', {}, 'Copied'), text.length > 80 ? text.slice(0, 80) + '…' : text);
      return true;
    } catch (e) {
      toast('error', t('errors.generic', {}, 'Something went wrong'));
      return false;
    }
  }
  function safeUrl(u) {
    const s = String(u || '');
    return /^https?:\/\//i.test(s) ? s : '';
  }
  function debounce(fn, wait) {
    let timer = null;
    return function (...args) {
      clearTimeout(timer);
      timer = setTimeout(() => fn.apply(this, args), wait);
    };
  }

  HP.state = state;
  HP.t = t;
  HP.i18n = i18n;
  HP.theme = theme;
  HP.toast = toast;
  HP.formatDate = formatDate;
  HP.formatDateTime = formatDateTime;
  HP.formatMonth = formatMonth;
  HP.formatBytes = formatBytes;
  HP.formatNumber = formatNumber;
  HP.initials = initials;
  HP.statusLabel = statusLabel;
  HP.statusSeverity = statusSeverity;
  HP.slugify = slugify;
  HP.toLocalInput = toLocalInput;
  HP.fromLocalInput = fromLocalInput;
  HP.copyText = copyText;
  HP.debounce = debounce;
  HP.safeUrl = safeUrl;

  parseHash();
})();
