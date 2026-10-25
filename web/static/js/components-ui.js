(function () {
  'use strict';
  const HP = window.HP;
  const { computed, reactive, ref, watch, onMounted } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  components.PublicToolbar = {
    name: 'PublicToolbar',
    setup() {
      const state = HP.state;
      const lang = computed(() => HP.i18n.lang);
      const isDark = computed(() => HP.theme.resolved === 'dark');
      const languages = computed(() => state.languages || []);
      async function changeLang(e) {
        const code = e && e.value ? e.value : e;
        await HP.setLocale(code, { persistUser: false });
      }
      return { state, lang, isDark, languages, changeLang, toggleTheme: HP.toggleTheme, t };
    },
    template: `
      <div class="public-toolbar">
        <PSelect :modelValue="lang" @update:modelValue="changeLang" :options="languages"
          optionLabel="nativeName" optionValue="code" class="lang-select" aria-label="Language" />
        <PButton rounded text :icon="isDark ? 'pi pi-sun' : 'pi pi-moon'" @click="toggleTheme"
          :aria-label="isDark ? t('common.light') : t('common.dark')" :title="isDark ? t('common.light') : t('common.dark')" />
      </div>
    `
  };

  components.SetupWizard = {
    name: 'SetupWizard',
    emits: ['done'],
    setup(props, { emit }) {
      const state = HP.state;
      const form = reactive({
        username: '',
        email: '',
        password: '',
        confirm: '',
        language: HP.i18n.lang || state.defaults.language || 'en'
      });
      const loading = ref(false);
      const error = ref('');
      const languages = computed(() => state.languages || []);
      async function changeLang(code) {
        form.language = code;
        await HP.setLocale(code, { persistUser: false });
      }
      async function submit() {
        error.value = '';
        if (!form.username.trim()) { error.value = t('auth.username_required', {}, 'Enter a username'); return; }
        if (form.password.length < 8) { error.value = t('errors.weak_password', {}, 'Password must be at least 8 characters'); return; }
        if (form.password !== form.confirm) { error.value = t('auth.password_mismatch', {}, 'Passwords do not match'); return; }
        loading.value = true;
        try {
          const data = await HP.api('/setup', {
            method: 'POST',
            body: {
              username: form.username.trim(),
              email: form.email.trim(),
              password: form.password,
              language: form.language
            }
          });
          emit('done', data);
        } catch (e) {
          error.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }
      return { form, loading, error, languages, submit, changeLang, t };
    },
    template: `
      <div class="auth-layout">
        <div class="auth-toolbar"><PublicToolbar /></div>
        <div class="auth-grid">
          <section class="auth-brand-panel">
            <div class="brand brand-large">
              <span class="brand-mark"><i class="pi pi-th-large"></i></span>
              <span class="brand-name">Hugo Panel</span>
            </div>
            <div class="auth-brand-copy">
              <span class="eyebrow"><span class="live-dot"></span>{{ t('auth.first_run', {}, 'First run') }}</span>
              <h1>{{ t('auth.setup_headline', {}, 'Your Hugo site, beautifully managed.') }}</h1>
              <p>{{ t('auth.setup_lead', {}, 'Write, publish and shape your site from one calm workspace. No config files, no terminal.') }}</p>
              <ul class="feature-list">
                <li><i class="pi pi-pencil"></i><span>{{ t('auth.feature_content', {}, 'Markdown content with a visual editor') }}</span></li>
                <li><i class="pi pi-palette"></i><span>{{ t('auth.feature_themes', {}, 'Themes, plugins and site settings') }}</span></li>
                <li><i class="pi pi-mobile"></i><span>{{ t('auth.feature_mobile', {}, 'Installable, mobile-ready and offline-aware') }}</span></li>
              </ul>
            </div>
            <div class="auth-footnote"><i class="pi pi-shield"></i><span>{{ t('auth.security_note', {}, 'Your data stays on your server in SQLite.') }}</span></div>
          </section>
          <section class="auth-card">
            <div class="auth-card-head">
              <span class="eyebrow">{{ t('auth.step_setup', {}, 'Setup') }}</span>
              <h2>{{ t('auth.setup_title', {}, 'Create your administrator') }}</h2>
              <p>{{ t('auth.setup_subtitle', {}, 'This account manages your Hugo site. You can change these details later.') }}</p>
            </div>
            <form class="form-stack" @submit.prevent="submit">
              <label class="field">
                <span>{{ t('auth.username', {}, 'Username') }}</span>
                <PInputText v-model="form.username" autocomplete="username" :placeholder="t('auth.username_hint', {}, 'e.g. kaiyuan')" autofocus />
              </label>
              <label class="field">
                <span>{{ t('auth.email', {}, 'Email') }} <em>{{ t('common.optional', {}, 'optional') }}</em></span>
                <PInputText v-model="form.email" type="email" autocomplete="email" placeholder="you@example.com" />
              </label>
              <div class="field-grid">
                <label class="field">
                  <span>{{ t('auth.password', {}, 'Password') }}</span>
                  <PPassword v-model="form.password" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" />
                </label>
                <label class="field">
                  <span>{{ t('auth.confirm_password', {}, 'Confirm password') }}</span>
                  <PPassword v-model="form.confirm" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" />
                </label>
              </div>
              <p class="field-hint"><i class="pi pi-info-circle"></i>{{ t('auth.password_hint', {}, 'At least 8 characters. Use a unique passphrase.') }}</p>
              <label class="field">
                <span>{{ t('common.language', {}, 'Language') }}</span>
                <PSelect :modelValue="form.language" @update:modelValue="changeLang" :options="languages"
                  optionLabel="nativeName" optionValue="code" class="w-full" />
              </label>
              <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
              <PButton type="submit" :loading="loading" :label="t('auth.create_admin', {}, 'Create administrator')" icon="pi pi-arrow-right" iconPos="right" class="w-full btn-primary" />
            </form>
            <div class="auth-meta"><i class="pi pi-lock"></i>{{ t('auth.setup_footer', {}, 'Only the first account can be created here.') }}</div>
          </section>
        </div>
      </div>
    `
  };

  components.AuthView = {
    name: 'AuthView',
    emits: ['authed'],
    setup(props, { emit }) {
      const state = HP.state;
      const route = HP.route;
      const mode = computed(() => {
        if (route.path === '/reset') return 'reset';
        if (route.path === '/forgot') return 'forgot';
        return 'login';
      });
      const loginForm = reactive({ username: '', password: '' });
      const forgotForm = reactive({ login: '' });
      const resetForm = reactive({ password: '', confirm: '' });
      const loading = ref(false);
      const error = ref('');
      const notice = ref('');
      const resetDone = ref(false);
      const token = computed(() => route.query.token || '');
      const isDark = computed(() => HP.theme.resolved === 'dark');

      watch(mode, () => { error.value = ''; notice.value = ''; loading.value = false; if (mode.value !== 'reset') resetDone.value = false; });

      async function login() {
        error.value = '';
        if (!loginForm.username.trim() || !loginForm.password) {
          error.value = t('auth.fill_login', {}, 'Enter your username and password');
          return;
        }
        loading.value = true;
        try {
          const data = await HP.api('/login', { method: 'POST', body: { username: loginForm.username.trim(), password: loginForm.password } });
          emit('authed', data);
        } catch (e) {
          error.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }
      async function forgot() {
        error.value = ''; notice.value = '';
        if (!forgotForm.login.trim()) {
          error.value = t('auth.enter_account', {}, 'Enter your email address or username');
          return;
        }
        loading.value = true;
        try {
          await HP.api('/password/forgot', { method: 'POST', body: { login: forgotForm.login.trim() } });
          notice.value = t('auth.forgot_sent', {}, 'If an account matches, a reset link has been sent. Check your inbox.');
        } catch (e) {
          error.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }
      async function reset() {
        error.value = '';
        if (!token.value) { error.value = t('errors.reset_invalid', {}, 'This reset link is invalid or has expired'); return; }
        if (resetForm.password.length < 8) { error.value = t('errors.weak_password', {}, 'Password must be at least 8 characters'); return; }
        if (resetForm.password !== resetForm.confirm) { error.value = t('auth.password_mismatch', {}, 'Passwords do not match'); return; }
        loading.value = true;
        try {
          await HP.api('/password/reset', { method: 'POST', body: { token: token.value, password: resetForm.password } });
          HP.state.user = null;
          HP.state.authenticated = false;
          HP.state.csrf = '';
          resetDone.value = true;
        } catch (e) {
          error.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }
      function go(path) { HP.navigate(path); }
      return { state, mode, loginForm, forgotForm, resetForm, loading, error, notice, resetDone, token, isDark, login, forgot, reset, go, t };
    },
    template: `
      <div class="auth-layout">
        <div class="auth-toolbar"><PublicToolbar /></div>
        <div class="auth-grid">
          <section class="auth-brand-panel">
            <div class="brand brand-large">
              <span class="brand-mark"><i class="pi pi-th-large"></i></span>
              <span class="brand-name">Hugo Panel</span>
            </div>
            <div class="auth-brand-copy">
              <span class="eyebrow"><span class="live-dot"></span>{{ t('app.workspace', {}, 'Workspace') }}</span>
              <h1>{{ t('auth.welcome_headline', {}, 'A quieter way to run your Hugo site.') }}</h1>
              <p>{{ t('auth.welcome_lead', {}, 'Content, themes and builds — together in one focused place.') }}</p>
              <div class="quote-card">
                <i class="pi pi-bolt"></i>
                <div><strong>{{ t('auth.quote_title', {}, 'Built for speed') }}</strong><span>{{ t('auth.quote_text', {}, 'Lightweight Go backend, instant SQLite, nothing hidden.') }}</span></div>
              </div>
            </div>
            <div class="auth-footnote"><i class="pi pi-shield"></i><span>{{ t('auth.security_note', {}, 'Your data stays on your server in SQLite.') }}</span></div>
          </section>
          <section class="auth-card">
            <template v-if="mode === 'login'">
              <div class="auth-card-head">
                <span class="eyebrow">{{ t('auth.overview', {}, 'Overview') }} <span class="live-dot"></span></span>
                <h2>{{ t('auth.welcome_back', {}, 'Welcome back') }}</h2>
                <p>{{ t('auth.login_subtitle', {}, 'Sign in to manage your Hugo site.') }}</p>
              </div>
              <form class="form-stack" @submit.prevent="login">
                <label class="field">
                  <span>{{ t('auth.username', {}, 'Username') }}</span>
                  <PInputText v-model="loginForm.username" autocomplete="username" autofocus />
                </label>
                <label class="field">
                  <span>{{ t('auth.password', {}, 'Password') }}</span>
                  <PPassword v-model="loginForm.password" :feedback="false" toggleMask inputClass="w-full" autocomplete="current-password" />
                </label>
                <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
                <PButton type="submit" :loading="loading" :label="t('auth.sign_in', {}, 'Sign in')" icon="pi pi-arrow-right" iconPos="right" class="w-full btn-primary" />
              </form>
              <div class="auth-links">
                <button class="link-button" type="button" @click="go('/forgot')">{{ t('auth.forgot_password', {}, 'Forgot password?') }}</button>
                <span class="muted">{{ t('auth.smtp_hint_short', {}, 'Password recovery requires SMTP.') }}</span>
              </div>
            </template>
            <template v-else-if="mode === 'forgot'">
              <div class="auth-card-head">
                <span class="eyebrow">{{ t('auth.account_recovery', {}, 'Account recovery') }}</span>
                <h2>{{ t('auth.forgot_title', {}, 'Reset your password') }}</h2>
                <p>{{ t('auth.forgot_subtitle', {}, 'Enter your account email or username and we will send a reset link if SMTP is configured.') }}</p>
              </div>
              <PMessage v-if="!state.smtpConfigured" severity="warn" :closable="false">{{ t('auth.smtp_not_configured', {}, 'SMTP is not configured yet. Ask an administrator to set it up in Settings.') }}</PMessage>
              <form class="form-stack" @submit.prevent="forgot">
                <label class="field">
                  <span>{{ t('auth.email_or_username', {}, 'Email or username') }}</span>
                  <PInputText v-model="forgotForm.login" autofocus />
                </label>
                <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
                <PMessage v-if="notice" severity="success" :closable="false">{{ notice }}</PMessage>
                <PButton type="submit" :loading="loading" :label="t('auth.send_reset', {}, 'Send reset link')" icon="pi pi-send" class="w-full btn-primary" />
              </form>
              <div class="auth-links"><button class="link-button" type="button" @click="go('/login')"><i class="pi pi-arrow-left"></i>{{ t('auth.back_to_login', {}, 'Back to sign in') }}</button></div>
            </template>
            <template v-else>
              <div v-if="!resetDone" class="auth-card-head">
                <span class="eyebrow">{{ t('auth.account_recovery', {}, 'Account recovery') }}</span>
                <h2>{{ t('auth.reset_title', {}, 'Choose a new password') }}</h2>
                <p>{{ t('auth.reset_subtitle', {}, 'Your reset link expires after one hour.') }}</p>
              </div>
              <template v-if="resetDone">
                <div class="auth-success">
                  <span class="success-icon"><i class="pi pi-check"></i></span>
                  <span class="eyebrow">{{ t('auth.all_set', {}, 'All set') }}</span>
                  <h2>{{ t('auth.reset_done', {}, 'Password updated') }}</h2>
                  <p>{{ t('auth.reset_done_text', {}, 'You can now sign in with your new password.') }}</p>
                  <PButton :label="t('auth.sign_in', {}, 'Sign in')" icon="pi pi-arrow-right" iconPos="right" class="w-full btn-primary" @click="go('/login')" />
                </div>
              </template>
              <form v-else class="form-stack" @submit.prevent="reset">
                <PMessage v-if="!token" severity="error" :closable="false">{{ t('errors.reset_invalid', {}, 'This reset link is invalid or has expired') }}</PMessage>
                <label class="field">
                  <span>{{ t('auth.new_password', {}, 'New password') }}</span>
                  <PPassword v-model="resetForm.password" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" />
                </label>
                <label class="field">
                  <span>{{ t('auth.confirm_password', {}, 'Confirm password') }}</span>
                  <PPassword v-model="resetForm.confirm" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" />
                </label>
                <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
                <PButton type="submit" :loading="loading" :label="t('auth.update_password', {}, 'Update password')" icon="pi pi-check" class="w-full btn-primary" />
              </form>
              <div class="auth-links"><button class="link-button" type="button" @click="go('/login')"><i class="pi pi-arrow-left"></i>{{ t('auth.back_to_login', {}, 'Back to sign in') }}</button></div>
            </template>
            <div class="auth-meta"><i class="pi pi-lock"></i>{{ t('auth.secure_connection', {}, 'Protected by a secure, HTTP-only session.') }}</div>
          </section>
        </div>
      </div>
    `
  };

  components.StatusTag = {
    name: 'StatusTag',
    props: { status: { type: String, default: '' } },
    setup(props) {
      const label = computed(() => HP.statusLabel(props.status));
      const severity = computed(() => HP.statusSeverity(props.status));
      return { label, severity };
    },
    template: `<PTag :value="label" :severity="severity" rounded />`
  };

  components.PageHeader = {
    name: 'PageHeader',
    props: { eyebrow: String, title: String, subtitle: String, live: Boolean },
    template: `
      <header class="page-header">
        <div class="page-header-copy">
          <span v-if="eyebrow" class="eyebrow"><span v-if="live" class="live-dot"></span>{{ eyebrow }}</span>
          <h1>{{ title }}</h1>
          <p v-if="subtitle">{{ subtitle }}</p>
        </div>
        <div class="page-header-actions"><slot name="actions"></slot></div>
      </header>
    `
  };

  components.EmptyState = {
    name: 'EmptyState',
    props: { icon: { type: String, default: 'pi pi-inbox' }, title: String, text: String },
    template: `
      <div class="empty-state">
        <span class="empty-icon"><i :class="icon"></i></span>
        <h3>{{ title }}</h3>
        <p v-if="text">{{ text }}</p>
        <div class="empty-actions"><slot></slot></div>
      </div>
    `
  };
})();

(function () {
  'use strict';
  const HP = window.HP;
  const { computed, ref, watch, onMounted } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  components.AppShell = {
    name: 'AppShell',
    setup() {
      const state = HP.state;
      const route = HP.route;
      const repoURL = 'https://github.com/Kaiyuan/hugo-panel';
      const confirm = PrimeVue.useConfirm();
      const collapsed = ref(localStorage.getItem('hp_sidebar') === 'collapsed');
      const mobileOpen = ref(false);
      const search = ref('');
      const userMenu = ref(null);
      const langMenu = ref(null);
      const isDark = computed(() => HP.theme.resolved === 'dark');
      const langLabel = computed(() => {
        const lang = state.languages.find((l) => l.code === HP.i18n.lang);
        return lang ? (lang.code.split('-')[0] || lang.code).toUpperCase() : 'EN';
      });

      const navItems = computed(() => [
        { to: '/dashboard', icon: 'pi pi-home', label: t('nav.dashboard', {}, 'Dashboard') },
        { to: '/content', icon: 'pi pi-file-edit', label: t('nav.content', {}, 'Content') },
        { to: '/media', icon: 'pi pi-images', label: t('nav.media', {}, 'Media') },
        { to: '/themes', icon: 'pi pi-palette', label: t('nav.themes', {}, 'Themes') },
        { to: '/plugins', icon: 'pi pi-box', label: t('nav.plugins', {}, 'Plugins') },
        { to: '/build', icon: 'pi pi-cloud-upload', label: t('nav.build', {}, 'Build') },
        { to: '/settings', icon: 'pi pi-cog', label: t('nav.settings', {}, 'Settings') }
      ]);

      function isActive(item) {
        if (item.to === '/content') return route.path.startsWith('/content');
        return route.path === item.to;
      }

      const pageTitle = computed(() => {
        switch (route.path) {
          case '/dashboard': return t('nav.dashboard', {}, 'Dashboard');
          case '/content': return t('nav.content', {}, 'Content');
          case '/content/new': return t('content.new', {}, 'New content');
          case '/content/edit': return t('content.edit', {}, 'Edit content');
          case '/media': return t('nav.media', {}, 'Media');
          case '/themes': return t('nav.themes', {}, 'Themes');
          case '/plugins': return t('nav.plugins', {}, 'Plugins');
          case '/build': return t('nav.build', {}, 'Build');
          case '/settings': return t('nav.settings', {}, 'Settings');
          default: return t('nav.dashboard', {}, 'Dashboard');
        }
      });

      const pageComponent = computed(() => {
        switch (route.path) {
          case '/dashboard': return 'DashboardPage';
          case '/content': return 'ContentPage';
          case '/content/new':
          case '/content/edit': return 'ContentEditor';
          case '/media': return 'MediaPage';
          case '/themes': return 'ThemesPage';
          case '/plugins': return 'PluginsPage';
          case '/build': return 'BuildPage';
          case '/settings': return 'SettingsPage';
          default: return 'DashboardPage';
        }
      });

      function navigate(path) {
        mobileOpen.value = false;
        HP.navigate(path);
      }
      function toggleCollapsed() {
        collapsed.value = !collapsed.value;
        localStorage.setItem('hp_sidebar', collapsed.value ? 'collapsed' : 'expanded');
      }
      function submitSearch() {
        const q = search.value.trim();
        HP.navigate('/content', q ? { q } : {});
        search.value = '';
        mobileOpen.value = false;
      }
      const languageItems = computed(() => (state.languages || []).map((l) => ({
        label: l.nativeName || l.name,
        icon: l.code === HP.i18n.lang ? 'pi pi-check' : 'pi pi-globe',
        command: () => HP.setLocale(l.code)
      })));
      const userMenuItems = computed(() => [
        { label: t('nav.profile', {}, 'Profile'), icon: 'pi pi-user', command: () => HP.navigate('/settings', { tab: 'profile' }) },
        { label: t('nav.settings', {}, 'Settings'), icon: 'pi pi-cog', command: () => HP.navigate('/settings') },
        { separator: true },
        { label: t('nav.logout', {}, 'Sign out'), icon: 'pi pi-sign-out', command: confirmLogout }
      ]);
      function confirmLogout() {
        confirm.require({
          message: t('auth.logout_confirm', {}, 'Sign out of Hugo Panel?'),
          header: t('nav.logout', {}, 'Sign out'),
          icon: 'pi pi-sign-out',
          acceptLabel: t('nav.logout', {}, 'Sign out'),
          rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: () => HP.logout()
        });
      }
      function openUserMenu(e) { userMenu.value.toggle(e); }
      function openLangMenu(e) { langMenu.value.toggle(e); }
      watch(() => route.path, () => { mobileOpen.value = false; });
      onMounted(() => { document.documentElement.classList.toggle('is-collapsed-pref', collapsed.value); });
      return {
        state, route, repoURL, collapsed, mobileOpen, search, userMenu, langMenu, isDark, langLabel,
        navItems, isActive, pageTitle, pageComponent, navigate, toggleCollapsed, submitSearch,
        languageItems, userMenuItems, openUserMenu, openLangMenu, toggleTheme: () => HP.setTheme(isDark.value ? 'light' : 'dark'),
        installApp: HP.installApp, initials: HP.initials, reload: () => location.reload(), t
      };
    },
    template: `
      <div class="app-shell" :class="{ 'is-collapsed': collapsed, 'is-mobile-open': mobileOpen }">
        <aside class="sidebar">
          <button class="brand brand-sidebar" @click="navigate('/dashboard')" :title="t('nav.dashboard')">
            <span class="brand-mark"><i class="pi pi-th-large"></i></span>
            <span class="brand-text">
              <span class="brand-name">Hugo Panel</span>
              <span class="brand-version">v{{ state.version }}</span>
            </span>
          </button>
          <nav class="sidebar-nav">
            <button v-for="item in navItems" :key="item.to" class="nav-item" :class="{ active: isActive(item) }"
              @click="navigate(item.to)" :title="item.label">
              <i :class="item.icon"></i>
              <span>{{ item.label }}</span>
            </button>
          </nav>
          <div class="sidebar-bottom">
            <div class="sidebar-status">
              <span class="status-dot" :class="{ offline: state.offline, busy: false }"></span>
              <span>{{ state.offline ? t('common.offline') : t('common.online') }}</span>
            </div>
            <div class="sidebar-meta">
              <a class="sidebar-link" :href="repoURL" target="_blank" rel="noopener noreferrer" :title="t('nav.github', {}, 'GitHub')">
                <i class="pi pi-github"></i>
                <span>{{ t('nav.github', {}, 'GitHub') }}</span>
                <i class="pi pi-external-link sidebar-link-external"></i>
              </a>
              <span class="sidebar-version" :title="'v' + state.version">v{{ state.version }}</span>
            </div>
            <button class="user-card" @click="openUserMenu($event)">
              <span class="avatar">{{ initials(state.user.username) }}</span>
              <span class="user-card-text"><strong>{{ state.user.username }}</strong><small>{{ state.user.email || t('auth.administrator') }}</small></span>
              <i class="pi pi-ellipsis-v"></i>
            </button>
          </div>
        </aside>
        <div class="mobile-scrim" @click="mobileOpen = false"></div>
        <div class="workspace">
          <header class="topbar">
            <div class="topbar-left">
              <button class="icon-btn only-mobile" @click="mobileOpen = true" :aria-label="t('common.menu')"><i class="pi pi-bars"></i></button>
              <button class="icon-btn only-desktop" @click="toggleCollapsed" :aria-label="t('common.menu')"><i :class="collapsed ? 'pi pi-angle-right' : 'pi pi-angle-left'"></i></button>
              <div class="topbar-heading">
                <span class="eyebrow">{{ t('app.workspace', {}, 'Workspace') }}</span>
                <h1>{{ pageTitle }}</h1>
              </div>
            </div>
            <div class="topbar-actions">
              <div class="search-box">
                <i class="pi pi-search"></i>
                <input class="search-input" v-model="search" @keydown.enter="submitSearch" :placeholder="t('content.search_placeholder', {}, 'Search content…')" :aria-label="t('common.search')" />
              </div>
              <span v-if="state.offline" class="offline-chip"><span class="status-dot offline"></span>{{ t('common.offline') }}</span>
              <PButton v-if="state.installPrompt" rounded text icon="pi pi-download" @click="installApp" :title="t('pwa.install', {}, 'Install app')" />
              <button class="icon-btn" @click="openLangMenu($event)" :title="t('common.language')">
                <span class="lang-label">{{ langLabel }}</span>
              </button>
              <button class="icon-btn" @click="toggleTheme" :title="isDark ? t('common.light') : t('common.dark')">
                <i :class="isDark ? 'pi pi-sun' : 'pi pi-moon'"></i>
              </button>
              <button class="avatar-btn" @click="openUserMenu($event)" :aria-label="t('nav.profile')">
                <span class="avatar">{{ initials(state.user.username) }}</span>
              </button>
            </div>
          </header>
          <main class="page-container">
            <div v-if="state.swUpdate" class="app-banner update">
              <span><i class="pi pi-refresh"></i> {{ t('pwa.update_available', {}, 'A new version of Hugo Panel is ready.') }}</span>
              <button class="link-button" @click="reload()">{{ t('pwa.reload', {}, 'Reload') }}</button>
            </div>
            <div v-if="state.offline" class="app-banner offline">
              <span><i class="pi pi-wifi"></i> {{ t('pwa.offline_banner', {}, 'You are offline. Some actions are unavailable until the connection returns.') }}</span>
            </div>
            <component :is="pageComponent" :key="route.full" />
          </main>
        </div>
        <PMenu ref="userMenu" :model="userMenuItems" popup />
        <PMenu ref="langMenu" :model="languageItems" popup />
      </div>
    `
  };
})();
