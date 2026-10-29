(function () {
  'use strict';
  const HP = window.HP;
  const { ref, reactive, computed, onMounted } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  components.SettingsPage = {
    name: 'SettingsPage',
    setup() {
      const state = HP.state;
      const route = HP.route;
      const confirm = PrimeVue.useConfirm();
      const validTabs = ['general', 'email', 'translation', 'ai', 'languages', 'profile'];
      const tab = ref(validTabs.indexOf(route.query.tab) >= 0 ? route.query.tab : 'general');
      const settings = ref(null);
      const system = ref(null);
      const loading = ref(true);
      const error = ref('');
      const savingGeneral = ref(false);
      const savingEmail = ref(false);
      const savingTranslation = ref(false);
      const savingAI = ref(false);
      const testingAI = ref(false);
      const savingProfile = ref(false);
      const savingPassword = ref(false);
      const testingEmail = ref(false);
      const testTo = ref('');
      const general = reactive({ hugo_dir: '', hugo_binary: 'hugo', public_url: '', default_language: 'en', default_theme: 'system' });
      const email = reactive({
        enabled: false, host: '', port: 587, username: '', password: '', from: '', from_name: 'Hugo Panel',
        security: 'starttls', notifications_enabled: true, clear_password: false
      });
      const translation = reactive({ provider: '', endpoint: '', api_key: '', clear_key: false });
      const ai = reactive({ enabled: false, base_url: '', model: '', api_key: '', clear_key: false });
      const profile = reactive({ email: '', language: 'en', theme: 'system' });
      const password = reactive({ current: '', next: '', confirm: '' });
      const languages = computed(() => state.languages || []);
      const envOverrides = computed(() => (settings.value && settings.value.env_overrides) || []);
      const smtpEnvOverrides = computed(() => envOverrides.value.filter((key) => key.indexOf('smtp_') === 0));
      const langOptions = computed(() => languages.value.map((l) => ({ label: l.nativeName || l.name, value: l.code })));
      const themeOptions = computed(() => [
        { label: t('common.system', {}, 'System'), value: 'system' },
        { label: t('common.light', {}, 'Light'), value: 'light' },
        { label: t('common.dark', {}, 'Dark'), value: 'dark' }
      ]);
      const securityOptions = computed(() => [
        { label: t('settings.security_starttls', {}, 'STARTTLS (recommended)'), value: 'starttls' },
        { label: t('settings.security_tls', {}, 'Implicit TLS'), value: 'tls' },
        { label: t('settings.security_none', {}, 'None'), value: 'none' }
      ]);
      const providerOptions = computed(() => [
        { label: t('settings.translation_off', {}, 'Disabled'), value: '' },
        { label: 'LibreTranslate', value: 'libretranslate' },
        { label: 'DeepL', value: 'deepl' }
      ]);
      const addVisible = ref(false);
      const adding = ref(false);
      const addForm = reactive({ code: '', name: '', messages: null, count: 0, file: '' });
      const fileInput = ref(null);
      const addError = ref('');

      function isEnv(key) { return envOverrides.value.indexOf(key) >= 0; }
      function hasSmtpEnv() { return smtpEnvOverrides.value.length > 0; }
      function fill() {
        if (!settings.value) return;
        general.hugo_dir = settings.value.hugo_dir || '';
        general.hugo_binary = settings.value.hugo_binary || 'hugo';
        general.public_url = settings.value.public_url || '';
        general.default_language = settings.value.default_language || 'en';
        general.default_theme = settings.value.default_theme || 'system';
        email.enabled = !!settings.value.smtp.enabled;
        email.host = settings.value.smtp.host || '';
        email.port = settings.value.smtp.port || 587;
        email.username = settings.value.smtp.username || '';
        email.password = '';
        email.from = settings.value.smtp.from || '';
        email.from_name = settings.value.smtp.from_name || 'Hugo Panel';
        email.security = settings.value.smtp.security || 'starttls';
        email.notifications_enabled = !!settings.value.notifications_enabled;
        const tr = settings.value.translation || {};
        translation.provider = tr.provider || '';
        translation.endpoint = tr.endpoint || '';
        translation.api_key = '';
        translation.clear_key = false;
        const aiData = settings.value.ai || {};
        ai.enabled = !!aiData.enabled;
        ai.base_url = aiData.base_url || '';
        ai.model = aiData.model || '';
        ai.api_key = '';
        ai.clear_key = false;
        profile.email = state.user.email || '';
        profile.language = state.user.language || 'en';
        profile.theme = state.user.theme || 'system';
        testTo.value = state.user.email || '';
      }
      async function load() {
        loading.value = true;
        error.value = '';
        try {
          const [s, sys] = await Promise.all([HP.api('/settings'), HP.api('/system')]);
          settings.value = s;
          system.value = sys;
          fill();
        } catch (e) { error.value = HP.errorText(e); }
        finally { loading.value = false; }
      }
      async function saveGeneral() {
        savingGeneral.value = true;
        error.value = '';
        try {
          settings.value = await HP.api('/settings', {
            method: 'PUT',
            body: {
              hugo_dir: general.hugo_dir, hugo_binary: general.hugo_binary, public_url: general.public_url,
              default_language: general.default_language, default_theme: general.default_theme
            }
          });
          fill();
          HP.toast('success', t('settings.saved', {}, 'Settings saved'));
        } catch (e) { error.value = HP.errorText(e); HP.notifyError(e); }
        finally { savingGeneral.value = false; }
      }
      async function saveEmail() {
        savingEmail.value = true;
        error.value = '';
        try {
          const body = {
            smtp_enabled: email.enabled, smtp_host: email.host, smtp_port: Number(email.port) || 587,
            smtp_username: email.username, smtp_from: email.from, smtp_from_name: email.from_name,
            smtp_security: email.security, notifications_enabled: email.notifications_enabled,
            clear_smtp_password: email.clear_password
          };
          if (email.password) body.smtp_password = email.password;
          settings.value = await HP.api('/settings', { method: 'PUT', body });
          fill();
          state.smtpConfigured = settings.value.smtp.enabled && !!settings.value.smtp.host && !!settings.value.smtp.from;
          HP.toast('success', t('settings.saved', {}, 'Settings saved'));
        } catch (e) { error.value = HP.errorText(e); HP.notifyError(e); }
        finally { savingEmail.value = false; }
      }
      async function saveTranslation() {
        savingTranslation.value = true;
        error.value = '';
        try {
          const body = {
            translate_provider: translation.provider, translate_endpoint: translation.endpoint,
            clear_translate_api_key: translation.clear_key
          };
          if (translation.api_key) body.translate_api_key = translation.api_key;
          settings.value = await HP.api('/settings', { method: 'PUT', body });
          fill();
          HP.toast('success', t('settings.saved', {}, 'Settings saved'));
        } catch (e) { error.value = HP.errorText(e); HP.notifyError(e); }
        finally { savingTranslation.value = false; }
      }
      async function saveAI() {
        savingAI.value = true;
        error.value = '';
        try {
          const body = {
            ai_enabled: ai.enabled, ai_base_url: ai.base_url, ai_model: ai.model,
            clear_ai_api_key: ai.clear_key
          };
          if (ai.api_key) body.ai_api_key = ai.api_key;
          settings.value = await HP.api('/settings', { method: 'PUT', body });
          fill();
          HP.toast('success', t('settings.saved', {}, 'Settings saved'));
        } catch (e) { error.value = HP.errorText(e); HP.notifyError(e); }
        finally { savingAI.value = false; }
      }
      async function testAI() {
        testingAI.value = true;
        error.value = '';
        try {
          const res = await HP.api('/ai/test', {
            method: 'POST',
            body: { base_url: ai.base_url, model: ai.model, api_key: ai.api_key }
          });
          if (res && res.ok) {
            HP.toast('success', t('settings.ai_test_ok', { model: res.model }, 'Connection OK — {model}'));
          } else {
            HP.toast('warn', t('settings.ai_test_failed', {}, 'Connection failed'), (res && res.message) || '');
          }
        } catch (e) { error.value = HP.errorText(e); HP.notifyError(e); }
        finally { testingAI.value = false; }
      }
      async function testEmail() {
        if (!testTo.value.trim()) { HP.toast('error', t('settings.recipient_required', {}, 'Enter a recipient email')); return; }
        testingEmail.value = true;
        try {
          await HP.api('/settings/test-email', { method: 'POST', body: { to: testTo.value.trim() } });
          HP.toast('success', t('settings.test_sent', {}, 'Test email sent'), testTo.value.trim());
        } catch (e) { HP.notifyError(e); }
        finally { testingEmail.value = false; }
      }
      async function saveProfile() {
        savingProfile.value = true;
        try {
          const res = await HP.api('/me', { method: 'PUT', body: { email: profile.email, language: profile.language, theme: profile.theme } });
          state.user = res.user;
          await HP.loadLocale(res.user.language);
          HP.applyTheme(res.user.theme, { persistUser: false });
          HP.toast('success', t('settings.profile_saved', {}, 'Profile updated'));
        } catch (e) { HP.notifyError(e); }
        finally { savingProfile.value = false; }
      }
      async function changePassword() {
        if (password.next.length < 8) { HP.toast('error', t('errors.weak_password', {}, 'Password must be at least 8 characters')); return; }
        if (password.next !== password.confirm) { HP.toast('error', t('auth.password_mismatch', {}, 'Passwords do not match')); return; }
        savingPassword.value = true;
        try {
          await HP.api('/me/password', { method: 'POST', body: { current_password: password.current, new_password: password.next } });
          password.current = ''; password.next = ''; password.confirm = '';
          HP.toast('success', t('settings.password_changed', {}, 'Password updated'));
        } catch (e) { HP.notifyError(e); }
        finally { savingPassword.value = false; }
      }
      function openAdd() {
        addVisible.value = true;
        addError.value = '';
        addForm.code = '';
        addForm.name = '';
        addForm.messages = null;
        addForm.count = 0;
        addForm.file = '';
        if (fileInput.value) fileInput.value.value = '';
      }
      function chooseTranslationFile() { if (fileInput.value) fileInput.value.click(); }
      async function onTranslationFile(e) {
        addError.value = '';
        const file = e.target.files && e.target.files[0];
        if (!file) return;
        try {
          const text = await file.text();
          const parsed = JSON.parse(text);
          if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('invalid');
          const messages = {};
          let count = 0;
          Object.keys(parsed).forEach((key) => {
            if (key.charAt(0) === '_') {
              if (key === '_name' && !addForm.name) addForm.name = String(parsed[key]);
              if (key === '_native_name' && !addForm.name) addForm.name = String(parsed[key]);
            } else if (typeof parsed[key] === 'string') {
              messages[key] = parsed[key];
              count++;
            }
          });
          if (!count) throw new Error('empty');
          addForm.messages = messages;
          addForm.count = count;
          addForm.file = file.name;
          if (!addForm.code) addForm.code = file.name.replace(/\.json$/i, '').replace(/_/g, '-');
        } catch (err) {
          addForm.messages = null;
          addForm.count = 0;
          addError.value = t('settings.translation_file_invalid', {}, 'The file must be a flat JSON object of text keys and values.');
        }
      }
      async function addTranslation() {
        addError.value = '';
        if (!addForm.messages || !addForm.count) { addError.value = t('settings.translation_file_required', {}, 'Choose a translation JSON file first.'); return; }
        if (!addForm.code.trim() || !addForm.name.trim()) { addError.value = t('settings.translation_meta_required', {}, 'Enter a language code and display name.'); return; }
        adding.value = true;
        try {
          const res = await HP.api('/locales', { method: 'POST', body: { code: addForm.code.trim(), name: addForm.name.trim(), messages: addForm.messages } });
          state.languages = res.languages || state.languages;
          addVisible.value = false;
          HP.toast('success', t('settings.translation_added', {}, 'Translation added'));
          if (HP.i18n.lang === addForm.code.trim()) await HP.loadLocale(addForm.code.trim());
        } catch (e) { addError.value = HP.errorText(e); }
        finally { adding.value = false; }
      }
      async function downloadTemplate() {
        try {
          const messages = await HP.api('/locales/template');
          const blob = new Blob([JSON.stringify(messages, null, 2) + '\n'], { type: 'application/json' });
          const a = document.createElement('a');
          a.href = URL.createObjectURL(blob);
          a.download = 'en.json';
          document.body.appendChild(a); a.click(); a.remove();
          setTimeout(() => URL.revokeObjectURL(a.href), 2000);
        } catch (e) { HP.notifyError(e); }
      }
      function deleteTranslation(lang) {
        confirm.require({
          message: t('settings.delete_translation_confirm', { name: lang.nativeName }, 'Delete the custom translation “{name}”?'),
          header: t('settings.delete_translation', {}, 'Delete translation'),
          icon: 'pi pi-trash', acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'), rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            try {
              const res = await HP.api('/locales?code=' + encodeURIComponent(lang.code), { method: 'DELETE' });
              state.languages = res.languages || state.languages;
              if (HP.i18n.lang === lang.code) await HP.setLocale('en', { persistUser: false });
              if (state.user && state.user.language === lang.code) {
                try {
                  const me = await HP.api('/me', { method: 'PUT', body: { language: 'en' } });
                  state.user = me.user;
                  profile.language = 'en';
                } catch (e) {}
              }
              HP.toast('success', t('settings.translation_deleted', {}, 'Translation deleted'));
            } catch (e) { HP.notifyError(e); }
          }
        });
      }
      function uptime(seconds) {
        if (!seconds) return '—';
        const d = Math.floor(seconds / 86400);
        const h = Math.floor((seconds % 86400) / 3600);
        const m = Math.floor((seconds % 3600) / 60);
        if (d) return d + 'd ' + h + 'h';
        if (h) return h + 'h ' + m + 'm';
        return m + 'm';
      }
      function userSince(ts) { return ts ? HP.formatDate(new Date(ts * 1000).toISOString()) : '—'; }
      onMounted(load);
      return {
        state, tab, settings, system, loading, error, savingGeneral, savingEmail, savingTranslation, savingAI, savingProfile, savingPassword,
        testingEmail, testingAI, testTo, general, email, translation, ai, profile, password, languages, envOverrides, smtpEnvOverrides,
        langOptions, themeOptions, securityOptions, providerOptions, addVisible, adding, addForm, fileInput, addError,
        isEnv, hasSmtpEnv, load, saveGeneral, saveEmail, saveTranslation, saveAI, testAI, testEmail, saveProfile, changePassword, openAdd,
        chooseTranslationFile, onTranslationFile, addTranslation, downloadTemplate, deleteTranslation,
        uptime, userSince, initials: HP.initials, formatDateTime: HP.formatDateTime, formatBytes: HP.formatBytes, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('settings.system', {}, 'System')" :title="t('settings.title', {}, 'Settings')" :subtitle="t('settings.subtitle', {}, 'Configure Hugo, email delivery, languages and your account.')">
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="load" :title="t('common.refresh')" />
          </template>
        </PageHeader>

        <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
        <div v-if="loading" class="editor-loading"><PProgressSpinner strokeWidth="3" /><span>{{ t('common.loading', {}, 'Loading…') }}</span></div>

        <PTabs v-else v-model:value="tab" class="settings-tabs">
          <PTabList>
            <PTab value="general"><i class="pi pi-sliders-h"></i> {{ t('settings.general', {}, 'General') }}</PTab>
            <PTab value="email"><i class="pi pi-envelope"></i> {{ t('settings.email', {}, 'Email') }}</PTab>
            <PTab value="translation"><i class="pi pi-language"></i> {{ t('settings.translation', {}, 'Translation') }}</PTab>
            <PTab value="ai"><i class="pi pi-bolt"></i> {{ t('settings.ai', {}, 'AI') }}</PTab>
            <PTab value="languages"><i class="pi pi-language"></i> {{ t('settings.languages', {}, 'Languages') }}</PTab>
            <PTab value="profile"><i class="pi pi-user"></i> {{ t('settings.profile', {}, 'Profile') }}</PTab>
          </PTabList>
          <PTabPanels>
            <PTabPanel value="general">
              <div class="settings-grid">
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.hugo', {}, 'Hugo environment') }}</h2><p>{{ t('settings.hugo_hint', {}, 'Where the panel reads content and writes builds.') }}</p></div></div>
                    <div class="form-stack">
                      <label class="field"><span>{{ t('settings.hugo_dir', {}, 'Hugo site directory') }}</span><PInputText v-model="general.hugo_dir" class="mono-input" :placeholder="'/app/hugo'" /><small class="field-hint">{{ t('settings.hugo_dir_hint', {}, 'Default in Docker: /app/hugo') }}</small></label>
                      <label class="field"><span>{{ t('settings.hugo_binary', {}, 'Hugo binary') }}</span><PInputText v-model="general.hugo_binary" class="mono-input" placeholder="hugo" /><small class="field-hint">{{ t('settings.hugo_binary_hint', { version: system.hugo_min_version || '0.169.0' }, 'Hugo {version} or newer is required for current themes.') }}</small></label>
                      <label class="field"><span>{{ t('settings.public_url', {}, 'Public panel URL') }} <em v-if="isEnv('public_url')" class="env-badge">ENV</em></span><PInputText v-model="general.public_url" :disabled="isEnv('public_url')" class="mono-input" placeholder="https://panel.example.com" /><small class="field-hint">{{ t('settings.public_url_hint', {}, 'Used in password reset links. Leave blank to infer from the request.') }}</small></label>
                      <div class="field-grid">
                        <label class="field"><span>{{ t('settings.default_language', {}, 'Default language') }}</span><PSelect v-model="general.default_language" :options="langOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
                        <label class="field"><span>{{ t('settings.default_theme', {}, 'Default theme') }}</span><PSelect v-model="general.default_theme" :options="themeOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
                      </div>
                      <PButton :label="t('common.save_changes', {}, 'Save changes')" icon="pi pi-check" :loading="savingGeneral" @click="saveGeneral" class="btn-primary" />
                    </div>
                  </template>
                </PCard>
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.system_info', {}, 'System information') }}</h2><p>{{ t('settings.system_info_hint', {}, 'Runtime versions and paths.') }}</p></div></div>
                    <dl class="key-list" v-if="system">
                      <div><dt>{{ t('settings.panel_version', {}, 'Panel version') }}</dt><dd>{{ system.panel_version }}</dd></div>
                      <div><dt>{{ t('settings.hugo_version', {}, 'Hugo') }}</dt><dd>{{ system.hugo_available ? system.hugo_version : t('settings.not_found', {}, 'Not found') }}</dd></div>
                      <div><dt>{{ t('settings.go_version', {}, 'Go') }}</dt><dd>{{ system.go_version }}</dd></div>
                      <div><dt>{{ t('settings.uptime', {}, 'Uptime') }}</dt><dd>{{ uptime(system.uptime_seconds) }}</dd></div>
                      <div><dt>{{ t('settings.platform', {}, 'Platform') }}</dt><dd>{{ system.os }}/{{ system.arch }}</dd></div>
                      <div><dt>{{ t('settings.go_available', {}, 'Go toolchain') }}</dt><dd>{{ system.go_available ? t('common.yes', {}, 'Yes') : t('common.no', {}, 'No') }}</dd></div>
                      <div><dt>{{ t('settings.git_available', {}, 'Git') }}</dt><dd>{{ system.git_available ? t('common.yes', {}, 'Yes') : t('common.no', {}, 'No') }}</dd></div>
                      <div><dt>{{ t('settings.data_dir', {}, 'Data directory') }}</dt><dd class="break-all">{{ system.data_dir }}</dd></div>
                      <div><dt>{{ t('settings.config_path', {}, 'Config file') }}</dt><dd class="break-all">{{ system.config_path }}</dd></div>
                    </dl>
                  </template>
                </PCard>
              </div>
            </PTabPanel>

            <PTabPanel value="email">
              <div class="settings-grid">
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.smtp', {}, 'SMTP delivery') }}</h2><p>{{ t('settings.smtp_hint', {}, 'Used for password recovery and build notifications.') }}</p></div><PTag v-if="hasSmtpEnv()" :value="t('settings.env_managed', {}, 'Managed by environment')" severity="info" /></div>
                    <PMessage v-if="hasSmtpEnv()" severity="info" :closable="false">{{ t('settings.env_override_hint', {}, 'Some SMTP values are set by Docker environment variables and cannot be changed here.') }}</PMessage>
                    <div class="switch-row">
                      <div><strong>{{ t('settings.smtp_enabled', {}, 'Enable SMTP') }}</strong><small>{{ t('settings.smtp_enabled_hint', {}, 'Turn on email delivery') }}</small></div>
                      <PToggleSwitch v-model="email.enabled" :disabled="isEnv('smtp_enabled')" />
                    </div>
                    <div class="field-grid">
                      <label class="field"><span>{{ t('settings.smtp_host', {}, 'Host') }} <em v-if="isEnv('smtp_host')" class="env-badge">ENV</em></span><PInputText v-model="email.host" :disabled="isEnv('smtp_host')" placeholder="smtp.example.com" /></label>
                      <label class="field"><span>{{ t('settings.smtp_port', {}, 'Port') }} <em v-if="isEnv('smtp_port')" class="env-badge">ENV</em></span><PInputNumber v-model="email.port" :min="1" :max="65535" :disabled="isEnv('smtp_port')" class="w-full" /></label>
                    </div>
                    <label class="field"><span>{{ t('settings.smtp_security', {}, 'Security') }} <em v-if="isEnv('smtp_security')" class="env-badge">ENV</em></span><PSelect v-model="email.security" :options="securityOptions" optionLabel="label" optionValue="value" :disabled="isEnv('smtp_security')" class="w-full" /></label>
                    <div class="field-grid">
                      <label class="field"><span>{{ t('settings.smtp_username', {}, 'Username') }} <em v-if="isEnv('smtp_username')" class="env-badge">ENV</em></span><PInputText v-model="email.username" :disabled="isEnv('smtp_username')" autocomplete="off" /></label>
                      <label class="field"><span>{{ t('settings.smtp_password', {}, 'Password') }} <em v-if="isEnv('smtp_password')" class="env-badge">ENV</em></span><PPassword v-model="email.password" :feedback="false" toggleMask inputClass="w-full" :disabled="isEnv('smtp_password')" :placeholder="settings.smtp.password_set ? t('settings.password_keep', {}, 'Saved — leave blank to keep') : ''" autocomplete="new-password" /></label>
                    </div>
                    <div class="field-grid">
                      <label class="field"><span>{{ t('settings.smtp_from', {}, 'From address') }} <em v-if="isEnv('smtp_from')" class="env-badge">ENV</em></span><PInputText v-model="email.from" :disabled="isEnv('smtp_from')" placeholder="notifications@example.com" /></label>
                      <label class="field"><span>{{ t('settings.smtp_from_name', {}, 'From name') }} <em v-if="isEnv('smtp_from_name')" class="env-badge">ENV</em></span><PInputText v-model="email.from_name" :disabled="isEnv('smtp_from_name')" /></label>
                    </div>
                    <div class="switch-row">
                      <div><strong>{{ t('settings.notifications', {}, 'Build notifications') }}</strong><small>{{ t('settings.notifications_hint', {}, 'Email administrators when a build fails') }}</small></div>
                      <PToggleSwitch v-model="email.notifications_enabled" />
                    </div>
                    <div v-if="settings.smtp.password_set && !isEnv('smtp_password')" class="check-row"><PCheckbox v-model="email.clear_password" binary /><span>{{ t('settings.clear_password', {}, 'Clear the saved password') }}</span></div>
                    <PButton :label="t('common.save_changes', {}, 'Save changes')" icon="pi pi-check" :loading="savingEmail" @click="saveEmail" class="btn-primary" />
                  </template>
                </PCard>
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.test_email', {}, 'Send a test email') }}</h2><p>{{ t('settings.test_email_hint', {}, 'Verify your SMTP configuration before relying on it.') }}</p></div></div>
                    <label class="field"><span>{{ t('settings.recipient', {}, 'Recipient') }}</span><PInputText v-model="testTo" type="email" placeholder="you@example.com" /></label>
                    <PButton :label="t('settings.send_test', {}, 'Send test email')" icon="pi pi-send" :loading="testingEmail" @click="testEmail" outlined />
                    <PMessage class="mt-3" severity="info" :closable="false">{{ t('settings.smtp_note', {}, 'Password reset links use your public panel URL. Build failure notices can be disabled above.') }}</PMessage>
                  </template>
                </PCard>
              </div>
            </PTabPanel>

            <PTabPanel value="translation">
              <div class="settings-grid">
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.translation_title', {}, 'Title translation') }}</h2><p>{{ t('settings.translation_hint', {}, 'Turns article titles into English file names in the editor.') }}</p></div></div>
                    <div class="form-stack">
                      <label class="field"><span>{{ t('settings.translation_provider', {}, 'Provider') }}</span><PSelect v-model="translation.provider" :options="providerOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
                      <label class="field"><span>{{ t('settings.translation_endpoint', {}, 'Endpoint') }}</span><PInputText v-model="translation.endpoint" class="mono-input" placeholder="https://libretranslate.com/translate" /><small class="field-hint">{{ t('settings.translation_endpoint_hint', {}, 'Leave blank to use the provider default.') }}</small></label>
                      <label class="field"><span>{{ t('settings.translation_api_key', {}, 'API key') }}</span><PPassword v-model="translation.api_key" :feedback="false" toggleMask inputClass="w-full" :placeholder="settings.translation.api_key_set ? t('settings.password_keep', {}, 'Saved — leave blank to keep') : ''" autocomplete="new-password" /><small class="field-hint">{{ t('settings.translation_api_key_hint', {}, 'Stored on the server and never returned by the API.') }}</small></label>
                      <div v-if="settings.translation.api_key_set" class="check-row"><PCheckbox v-model="translation.clear_key" binary /><span>{{ t('settings.translation_clear_key', {}, 'Remove the saved API key') }}</span></div>
                      <PButton :label="t('common.save_changes', {}, 'Save changes')" icon="pi pi-check" :loading="savingTranslation" @click="saveTranslation" class="btn-primary" />
                    </div>
                  </template>
                </PCard>
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.translation_help', {}, 'How it works') }}</h2><p>{{ t('settings.translation_help_hint', {}, 'What happens when no provider is set.') }}</p></div></div>
                    <PMessage severity="info" :closable="false">{{ t('settings.translation_note', {}, 'When a provider is configured, the “English” button in the editor translates the title and slugifies it. Without a provider, or if the request fails, English titles are still converted locally and Chinese titles fall back to a date-based name.') }}</PMessage>
                  </template>
                </PCard>
              </div>
            </PTabPanel>

            <PTabPanel value="ai">
              <div class="settings-grid">
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.ai_title', {}, 'AI provider') }}</h2><p>{{ t('settings.ai_hint', {}, 'Connect an OpenAI-compatible API for writing assistance and title translation.') }}</p></div></div>
                    <div class="switch-row">
                      <div><strong>{{ t('settings.ai_enabled', {}, 'Enable AI features') }}</strong><small>{{ t('settings.ai_enabled_hint', {}, 'Editor actions and title translation are sent to the configured provider.') }}</small></div>
                      <PToggleSwitch v-model="ai.enabled" />
                    </div>
                    <div class="form-stack">
                      <label class="field"><span>{{ t('settings.ai_base_url', {}, 'API base URL') }}</span><PInputText v-model="ai.base_url" class="mono-input" placeholder="https://api.openai.com/v1" /><small class="field-hint">{{ t('settings.ai_base_url_hint', {}, 'Base URL of an OpenAI-compatible API. A full /chat/completions endpoint is also accepted.') }}</small></label>
                      <label class="field"><span>{{ t('settings.ai_model', {}, 'Model') }}</span><PInputText v-model="ai.model" class="mono-input" placeholder="gpt-4o-mini" /><small class="field-hint">{{ t('settings.ai_model_hint', {}, 'Model name accepted by the provider, e.g. gpt-4o-mini or deepseek-chat.') }}</small></label>
                      <label class="field"><span>{{ t('settings.ai_api_key', {}, 'API key') }}</span><PPassword v-model="ai.api_key" :feedback="false" toggleMask inputClass="w-full" :placeholder="settings.ai.api_key_set ? t('settings.password_keep', {}, 'Saved — leave blank to keep') : ''" autocomplete="new-password" /><small class="field-hint">{{ t('settings.ai_api_key_hint', {}, 'Stored on the server and never returned by the API. Leave blank for local providers without a key.') }}</small></label>
                      <div v-if="settings.ai.api_key_set" class="check-row"><PCheckbox v-model="ai.clear_key" binary /><span>{{ t('settings.ai_clear_key', {}, 'Remove the saved API key') }}</span></div>
                      <div class="button-row">
                        <PButton :label="t('common.save_changes', {}, 'Save changes')" icon="pi pi-check" :loading="savingAI" @click="saveAI" class="btn-primary" />
                        <PButton :label="t('settings.ai_test', {}, 'Test connection')" icon="pi pi-bolt" :loading="testingAI" outlined @click="testAI" />
                      </div>
                    </div>
                  </template>
                </PCard>
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.ai_help', {}, 'Compatible providers') }}</h2><p>{{ t('settings.ai_help_hint', {}, 'Any OpenAI-compatible chat completions API works.') }}</p></div></div>
                    <PMessage severity="info" :closable="false">{{ t('settings.ai_note', {}, 'Works with OpenAI, DeepSeek, Moonshot, Qwen, SiliconFlow, OpenRouter, Ollama and LM Studio. When no dedicated translation provider is set, the AI also generates English file names. Article text is sent to the provider you configure.') }}</PMessage>
                  </template>
                </PCard>
              </div>
            </PTabPanel>

            <PTabPanel value="languages">
              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head">
                    <div><h2>{{ t('settings.translations', {}, 'Interface translations') }}</h2><p>{{ t('settings.translations_hint', {}, 'Each language is a standalone JSON file. Add your own without rebuilding.') }}</p></div>
                    <div class="head-actions">
                      <PButton text icon="pi pi-download" :label="t('settings.download_template', {}, 'English template')" @click="downloadTemplate" />
                      <PButton icon="pi pi-plus" :label="t('settings.add_translation', {}, 'Add translation')" @click="openAdd" />
                    </div>
                  </div>
                  <div class="language-list">
                    <div v-for="lang in languages" :key="lang.code" class="language-row">
                      <span class="language-flag">{{ lang.code.split('-')[0].toUpperCase() }}</span>
                      <div class="language-name"><strong>{{ lang.nativeName }}</strong><small>{{ lang.name }} · {{ lang.code }}</small></div>
                      <PTag :value="lang.builtin ? t('settings.builtin', {}, 'Built-in') : t('settings.custom', {}, 'Custom')" :severity="lang.builtin ? 'secondary' : 'success'" rounded />
                      <PButton v-if="!lang.builtin" text rounded severity="danger" icon="pi pi-trash" @click="deleteTranslation(lang)" :title="t('common.delete')" />
                    </div>
                  </div>
                  <PMessage severity="info" :closable="false" class="mt-3">
                    {{ t('settings.custom_locale_hint', {}, 'Custom files live in the locales folder inside the data directory and can also be added directly on disk.') }}
                    <span v-if="system" class="mono-inline">{{ system.data_dir }}/locales</span>
                  </PMessage>
                </template>
              </PCard>
            </PTabPanel>

            <PTabPanel value="profile">
              <div class="settings-grid">
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.account', {}, 'Your account') }}</h2><p>{{ t('settings.account_hint', {}, 'Profile and personal interface preferences.') }}</p></div><PTag :value="state.user.role" severity="secondary" /></div>
                    <div class="profile-head">
                      <span class="avatar large">{{ initials(state.user.username) }}</span>
                      <div><strong>{{ state.user.username }}</strong><small>{{ t('settings.member_since', { date: userSince(state.user.created_at) }, 'Member since {date}') }}</small></div>
                    </div>
                    <div class="form-stack">
                      <label class="field"><span>{{ t('auth.email', {}, 'Email') }}</span><PInputText v-model="profile.email" type="email" placeholder="you@example.com" /></label>
                      <div class="field-grid">
                        <label class="field"><span>{{ t('common.language', {}, 'Language') }}</span><PSelect v-model="profile.language" :options="langOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
                        <label class="field"><span>{{ t('common.theme', {}, 'Theme') }}</span><PSelect v-model="profile.theme" :options="themeOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
                      </div>
                      <PButton :label="t('settings.save_profile', {}, 'Save profile')" icon="pi pi-check" :loading="savingProfile" @click="saveProfile" class="btn-primary" />
                    </div>
                  </template>
                </PCard>
                <PCard class="panel-card">
                  <template #content>
                    <div class="panel-head"><div><h2>{{ t('settings.change_password', {}, 'Change password') }}</h2><p>{{ t('settings.change_password_hint', {}, 'Other sessions will be signed out.') }}</p></div></div>
                    <div class="form-stack">
                      <label class="field"><span>{{ t('settings.current_password', {}, 'Current password') }}</span><PPassword v-model="password.current" :feedback="false" toggleMask inputClass="w-full" autocomplete="current-password" /></label>
                      <label class="field"><span>{{ t('settings.new_password', {}, 'New password') }}</span><PPassword v-model="password.next" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" /></label>
                      <label class="field"><span>{{ t('auth.confirm_password', {}, 'Confirm password') }}</span><PPassword v-model="password.confirm" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" /></label>
                      <PButton :label="t('settings.update_password', {}, 'Update password')" icon="pi pi-lock" :loading="savingPassword" @click="changePassword" class="btn-primary" />
                    </div>
                  </template>
                </PCard>
              </div>
            </PTabPanel>
          </PTabPanels>
        </PTabs>

        <PDialog v-model:visible="addVisible" modal :header="t('settings.add_translation', {}, 'Add translation')" :style="{ width: '34rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <PMessage severity="info" :closable="false">{{ t('settings.add_translation_hint', {}, 'Upload a flat JSON file (English template recommended). Meta keys _name and _native_name are optional.') }}</PMessage>
            <button type="button" class="translation-picker" @click="chooseTranslationFile">
              <i class="pi pi-file-upload"></i>
              <span v-if="addForm.file"><strong>{{ addForm.file }}</strong><small>{{ t('settings.keys_loaded', { count: addForm.count }, '{count} keys loaded') }}</small></span>
              <span v-else><strong>{{ t('settings.choose_file', {}, 'Choose a JSON file') }}</strong><small>{{ t('settings.choose_file_hint', {}, 'Flat key/value strings only') }}</small></span>
            </button>
            <input ref="fileInput" type="file" accept="application/json,.json" class="hidden-input" @change="onTranslationFile" />
            <div class="field-grid">
              <label class="field"><span>{{ t('settings.language_code', {}, 'Language code') }}</span><PInputText v-model="addForm.code" placeholder="de" class="mono-input" /></label>
              <label class="field"><span>{{ t('settings.language_name', {}, 'Display name') }}</span><PInputText v-model="addForm.name" placeholder="Deutsch" /></label>
            </div>
            <PMessage v-if="addError" severity="error" :closable="false">{{ addError }}</PMessage>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="addVisible = false" />
            <PButton :label="t('settings.add_translation', {}, 'Add translation')" icon="pi pi-plus" :loading="adding" @click="addTranslation" />
          </template>
        </PDialog>
      </div>
    `
  };
})();
