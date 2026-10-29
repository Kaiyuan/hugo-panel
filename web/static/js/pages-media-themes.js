(function () {
  'use strict';
  const HP = window.HP;
  const { ref, reactive, computed, onMounted, watch, nextTick } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  function flattenParams(obj, prefix, out) {
    Object.keys(obj || {}).forEach((key) => {
      const value = obj[key];
      const path = prefix ? prefix + '.' + key : key;
      if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
        flattenParams(value, path, out);
      } else {
        let type = 'string';
        let display = value;
        if (Array.isArray(value)) { type = 'json'; display = JSON.stringify(value, null, 2); }
        else if (typeof value === 'boolean') { type = 'boolean'; }
        else if (typeof value === 'number') { type = 'number'; display = String(value); }
        else { display = value === undefined || value === null ? '' : String(value); }
        out.push({ key: path, type, value: display });
      }
    });
  }
  function setDeep(obj, key, value) {
    const parts = key.split('.');
    let cur = obj;
    for (let i = 0; i < parts.length - 1; i++) {
      const p = parts[i];
      if (!cur[p] || typeof cur[p] !== 'object' || Array.isArray(cur[p])) cur[p] = {};
      cur = cur[p];
    }
    cur[parts[parts.length - 1]] = value;
  }
  function paramsFromRows(rows) {
    const out = {};
    const seen = {};
    for (const row of rows) {
      const key = String(row.key || '').trim();
      if (!key) continue;
      if (seen[key]) throw new HP.ApiError('invalid_request', t('themes.duplicate_param', { key }, '“{key}” appears more than once.'));
      seen[key] = true;
      let value = row.value;
      if (row.type === 'number') {
        value = Number(String(value).trim());
        if (!isFinite(value)) throw new HP.ApiError('invalid_request', t('themes.invalid_number', { key }, '“{key}” must be a number.'));
      } else if (row.type === 'boolean') value = !!value;
      else if (row.type === 'json') {
        try { value = JSON.parse(value); }
        catch (e) { throw new HP.ApiError('invalid_request', t('themes.invalid_json', { key }, '“{key}” contains invalid JSON.')); }
      } else value = String(value === undefined || value === null ? '' : value);
      setDeep(out, key, value);
    }
    return out;
  }

  components.SiteConfigPanel = {
    name: 'SiteConfigPanel',
    emits: ['saved'],
    setup(props, { emit }) {
      const confirm = PrimeVue.useConfirm();
      const cfg = ref(null);
      const themes = ref([]);
      const loading = ref(true);
      const saving = ref(false);
      const error = ref('');
      const paramsRows = ref([]);
      const menuGroups = ref([]);
      const initVisible = ref(false);
      const initLoading = ref(false);
      const initForm = reactive({ title: '', base_url: '', language: 'en' });
      const hugoInfo = reactive({ checked: false, version: '', min: '', ok: true });
      const initError = ref('');
      const form = reactive({
        site_title: '', base_url: '', language_code: '', default_content_language: '', theme: ''
      });
      const themeOptions = computed(() => {
        const list = themes.value.map((x) => ({ label: x.title || x.name, value: x.name }));
        if (form.theme && !list.some((x) => x.value === form.theme)) list.unshift({ label: form.theme, value: form.theme });
        return [{ label: t('themes.no_theme', {}, 'No theme'), value: '' }].concat(list);
      });
      const typeOptions = computed(() => [
        { label: t('content.type_string', {}, 'Text'), value: 'string' },
        { label: t('content.type_number', {}, 'Number'), value: 'number' },
        { label: t('content.type_boolean', {}, 'Boolean'), value: 'boolean' },
        { label: t('content.type_json', {}, 'JSON'), value: 'json' }
      ]);
      async function load() {
        loading.value = true;
        error.value = '';
        try {
          const [config, themeData] = await Promise.all([HP.api('/hugo/config'), HP.api('/themes').catch(() => ({ themes: [] }))]);
          cfg.value = config;
          themes.value = themeData.themes || [];
          form.site_title = config.site_title || '';
          form.base_url = config.base_url || '';
          form.language_code = config.language_code || '';
          form.default_content_language = config.default_content_language || '';
          form.theme = config.theme || '';
          paramsRows.value = [];
          flattenParams(config.params || {}, '', paramsRows.value);
          menuGroups.value = Object.keys(config.menus || {}).map((name) => ({
            name,
            entries: (config.menus[name] || []).map((e) => ({
              name: e.name || '', page_ref: e.page_ref || '', url: e.url || '', weight: e.weight || 0,
              identifier: e.identifier || '', parent: e.parent || '', pre: e.pre || '', post: e.post || ''
            }))
          }));
          initForm.title = config.site_title || t('dashboard.default_site', {}, 'My Hugo site');
          initForm.base_url = config.base_url || '';
        } catch (e) { error.value = HP.errorText(e); }
        finally { loading.value = false; }
      }
      async function save() {
        error.value = '';
        saving.value = true;
        try {
          const params = paramsFromRows(paramsRows.value);
          const menus = {};
          for (const group of menuGroups.value) {
            const name = String(group.name || '').trim();
            if (!name) continue;
            menus[name] = group.entries.map((e) => ({
              name: e.name, page_ref: e.page_ref, url: e.url, weight: Number(e.weight) || 0,
              identifier: e.identifier, parent: e.parent, pre: e.pre, post: e.post
            })).filter((e) => e.name || e.url || e.page_ref);
          }
          const updated = await HP.api('/hugo/config', {
            method: 'PUT',
            body: {
              site_title: form.site_title, base_url: form.base_url, language_code: form.language_code,
              default_content_language: form.default_content_language, theme: form.theme, params, menus
            }
          });
          cfg.value = updated;
          HP.toast('success', t('themes.config_saved', {}, 'Theme configuration saved'));
          emit('saved');
          await load();
        } catch (e) { error.value = HP.errorText(e); HP.notifyError(e); }
        finally { saving.value = false; }
      }
      function addParam() { paramsRows.value.push({ key: '', type: 'string', value: '' }); }
      function removeParam(i) { paramsRows.value.splice(i, 1); }
      function addMenu() { menuGroups.value.push({ name: '', entries: [] }); }
      function removeMenu(i) { menuGroups.value.splice(i, 1); }
      function addEntry(group) { group.entries.push({ name: '', page_ref: '', url: '', weight: 0, identifier: '', parent: '', pre: '', post: '' }); }
      function removeEntry(group, i) { group.entries.splice(i, 1); }
      function openInit() {
        initError.value = '';
        initVisible.value = true;
        loadHugoInfo();
      }
      async function loadHugoInfo() {
        hugoInfo.checked = false;
        hugoInfo.version = '';
        hugoInfo.min = '';
        hugoInfo.ok = true;
        try {
          const sys = await HP.api('/system');
          hugoInfo.version = sys.hugo_version || '';
          hugoInfo.min = sys.hugo_min_version || '';
          hugoInfo.ok = sys.hugo_supported !== false;
          hugoInfo.checked = true;
        } catch (e) {
          // Leave the state unknown; the backend rejects initialization if the
          // binary is missing or too old.
        }
      }
      async function initSite() {
        initError.value = '';
        initLoading.value = true;
        try {
          await HP.api('/hugo/init', { method: 'POST', body: { title: initForm.title, base_url: initForm.base_url, language: initForm.language } });
          initVisible.value = false;
          HP.toast('success', t('themes.site_initialized', {}, 'Hugo site initialized'));
          emit('saved');
          await load();
        } catch (e) {
          initError.value = HP.errorText(e);
          loadHugoInfo();
        }
        finally { initLoading.value = false; }
      }
      onMounted(load);
      return {
        cfg, themes, loading, saving, error, form, paramsRows, menuGroups, themeOptions, typeOptions,
        initVisible, initLoading, initForm, hugoInfo, initError, load, save, addParam, removeParam, addMenu, removeMenu,
        addEntry, removeEntry, openInit, initSite, t
      };
    },
    template: `
      <div>
        <div v-if="loading" class="editor-loading"><PProgressSpinner strokeWidth="3" /><span>{{ t('common.loading', {}, 'Loading…') }}</span></div>
        <PMessage v-else-if="error" severity="error" :closable="false">{{ error }}</PMessage>
        <template v-else-if="cfg">
          <PMessage v-if="!cfg.exists" severity="warn" :closable="false" class="mb-3">
            {{ t('themes.not_initialized', {}, 'This folder does not contain a Hugo configuration yet.') }}
            <button class="link-button" @click="openInit">{{ t('themes.init_site', {}, 'Initialize site') }}</button>
          </PMessage>
          <div class="config-layout">
            <div class="config-main">
              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('themes.site_settings', {}, 'Site settings') }}</h2><p>{{ t('themes.site_settings_hint', {}, 'Core Hugo configuration written through a safe, backed-up editor.') }}</p></div><PTag :value="cfg.config_path || 'hugo.toml'" severity="secondary" /></div>
                  <div class="form-stack">
                    <div class="field-grid">
                      <label class="field"><span>{{ t('themes.site_title', {}, 'Site title') }}</span><PInputText v-model="form.site_title" /></label>
                      <label class="field"><span>{{ t('themes.active_theme', {}, 'Active theme') }}</span><PSelect v-model="form.theme" :options="themeOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
                    </div>
                    <label class="field"><span>{{ t('themes.base_url', {}, 'Base URL') }}</span><PInputText v-model="form.base_url" placeholder="https://example.com/" class="mono-input" /></label>
                    <div class="field-grid">
                      <label class="field"><span>{{ t('themes.language_code', {}, 'Language code') }}</span><PInputText v-model="form.language_code" placeholder="en" /></label>
                      <label class="field"><span>{{ t('themes.default_content_language', {}, 'Default content language') }}</span><PInputText v-model="form.default_content_language" placeholder="en" /></label>
                    </div>
                  </div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('themes.params', {}, 'Theme parameters') }}</h2><p>{{ t('themes.params_hint', {}, 'Nested values use dotted keys, e.g. colors.primary.') }}</p></div><PButton text icon="pi pi-plus" :label="t('themes.add_param', {}, 'Add parameter')" @click="addParam" /></div>
                  <div v-if="paramsRows.length === 0" class="empty-inline compact"><i class="pi pi-sliders-h"></i><span>{{ t('themes.no_params', {}, 'This site has no params yet.') }}</span></div>
                  <div v-for="(row, i) in paramsRows" :key="i" class="extra-row">
                    <PInputText v-model="row.key" placeholder="key" class="mono-input" />
                    <PSelect v-model="row.type" :options="typeOptions" optionLabel="label" optionValue="value" class="extra-type" />
                    <PToggleSwitch v-if="row.type === 'boolean'" v-model="row.value" />
                    <PTextarea v-else-if="row.type === 'json'" v-model="row.value" rows="2" autoResize class="mono-input" />
                    <PInputText v-else v-model="row.value" placeholder="value" />
                    <PButton text rounded severity="danger" icon="pi pi-times" @click="removeParam(i)" />
                  </div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('themes.menus', {}, 'Menus') }}</h2><p>{{ t('themes.menus_hint', {}, 'Navigation entries for your theme.') }}</p></div><PButton text icon="pi pi-plus" :label="t('themes.add_menu', {}, 'Add menu')" @click="addMenu" /></div>
                  <div v-if="menuGroups.length === 0" class="empty-inline compact"><i class="pi pi-bars"></i><span>{{ t('themes.no_menus', {}, 'No menus configured.') }}</span></div>
                  <div v-for="(group, gi) in menuGroups" :key="gi" class="menu-group">
                    <div class="menu-group-head">
                      <PInputText v-model="group.name" placeholder="main" class="mono-input menu-name" />
                      <PButton text icon="pi pi-plus" :label="t('themes.add_item', {}, 'Add item')" @click="addEntry(group)" />
                      <PButton text rounded severity="danger" icon="pi pi-trash" @click="removeMenu(gi)" />
                    </div>
                    <div v-for="(entry, ei) in group.entries" :key="ei" class="menu-entry-row">
                      <PInputText v-model="entry.name" :placeholder="t('themes.menu_name', {}, 'Name')" />
                      <PInputText v-model="entry.page_ref" :placeholder="t('themes.menu_page_ref', {}, 'Page reference')" class="mono-input" />
                      <PInputText v-model="entry.url" :placeholder="t('themes.menu_url', {}, 'URL')" class="mono-input" />
                      <PInputNumber v-model="entry.weight" :min="0" :max="10000" :showButtons="false" placeholder="0" class="menu-weight" />
                      <PButton text rounded severity="danger" icon="pi pi-times" @click="removeEntry(group, ei)" />
                    </div>
                  </div>
                </template>
              </PCard>
            </div>
            <aside class="config-side">
              <PCard class="panel-card sticky-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('themes.save_config', {}, 'Save configuration') }}</h2><p>{{ t('themes.backup_hint', {}, 'A timestamped backup is written before every change.') }}</p></div></div>
                  <dl class="key-list">
                    <div><dt>{{ t('themes.config_format', {}, 'Format') }}</dt><dd>{{ (cfg.format || 'toml').toUpperCase() }}</dd></div>
                    <div><dt>{{ t('themes.config_path', {}, 'File') }}</dt><dd class="break-all">{{ cfg.config_path }}</dd></div>
                  </dl>
                  <div class="action-stack">
                    <PButton class="w-full btn-primary" :label="t('common.save_changes', {}, 'Save changes')" icon="pi pi-check" :loading="saving" @click="save" />
                    <PButton class="w-full" outlined :label="t('themes.init_site', {}, 'Initialize site')" icon="pi pi-server" @click="openInit" />
                  </div>
                </template>
              </PCard>
            </aside>
          </div>
        </template>
        <PDialog v-model:visible="initVisible" modal :header="t('themes.init_site', {}, 'Initialize site')" :style="{ width: '30rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <PMessage severity="info" :closable="false">{{ t('themes.init_hint', {}, 'This creates the standard Hugo folders and a hugo.toml in your configured directory.') }}</PMessage>
            <PMessage v-if="hugoInfo.checked && !hugoInfo.version" severity="warn" :closable="false">{{ t('themes.hugo_missing', {}, 'Hugo was not found. Install the latest Hugo and set its path in Settings → General.') }}</PMessage>
            <PMessage v-else-if="hugoInfo.checked && !hugoInfo.ok" severity="error" :closable="false">{{ t('themes.hugo_outdated', { version: hugoInfo.version, required: hugoInfo.min }, 'Hugo {version} is too old. Version {required} or newer is required for current themes.') }}</PMessage>
            <small v-else-if="hugoInfo.checked && hugoInfo.version" class="field-hint"><i class="pi pi-info-circle"></i>{{ t('themes.hugo_detected', { version: hugoInfo.version }, 'Hugo {version} detected.') }}</small>
            <PMessage v-if="initError" severity="error" :closable="false">{{ initError }}</PMessage>
            <label class="field"><span>{{ t('themes.site_title', {}, 'Site title') }}</span><PInputText v-model="initForm.title" /></label>
            <label class="field"><span>{{ t('themes.base_url', {}, 'Base URL') }}</span><PInputText v-model="initForm.base_url" placeholder="https://example.com/" /></label>
            <label class="field"><span>{{ t('themes.language_code', {}, 'Language code') }}</span><PInputText v-model="initForm.language" placeholder="en" /></label>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="initVisible = false" />
            <PButton :label="t('themes.init_site', {}, 'Initialize site')" :loading="initLoading" :disabled="!hugoInfo.ok || (hugoInfo.checked && !hugoInfo.version)" @click="initSite" />
          </template>
        </PDialog>
      </div>
    `
  };

  components.ThemesPage = {
    name: 'ThemesPage',
    setup() {
      const route = HP.route;
      const confirm = PrimeVue.useConfirm();
      const tab = ref(route.query.tab === 'config' ? 'config' : 'installed');
      const themes = ref([]);
      const active = ref('');
      const loading = ref(true);
      const error = ref('');
      const installing = ref(false);
      const installVisible = ref(false);
      const installForm = reactive({ url: '', name: '' });
      const busy = ref({});

      async function load() {
        loading.value = true;
        error.value = '';
        try {
          const data = await HP.api('/themes');
          themes.value = data.themes || [];
          active.value = data.active || '';
        } catch (e) { error.value = HP.errorText(e); }
        finally { loading.value = false; }
      }
      async function activate(theme) {
        busy.value[theme.name] = true;
        try {
          await HP.api('/themes/activate', { method: 'POST', body: { name: theme.name } });
          HP.toast('success', t('themes.activated', { name: theme.name }, '{name} is now active'));
          await load();
        } catch (e) { HP.notifyError(e); }
        finally { busy.value[theme.name] = false; }
      }
      async function update(theme) {
        busy.value[theme.name] = true;
        try {
          const res = await HP.api('/themes/update', { method: 'POST', body: { name: theme.name } });
          HP.toast('success', t('themes.updated', { name: theme.name }, '{name} updated'), (res.output || '').slice(0, 180));
        } catch (e) { HP.notifyError(e); }
        finally { busy.value[theme.name] = false; }
      }
      function remove(theme) {
        confirm.require({
          message: t('themes.remove_confirm', { name: theme.name }, 'Remove the theme “{name}” from disk?'),
          header: t('themes.remove', {}, 'Remove theme'),
          icon: 'pi pi-trash', acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'), rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            try {
              await HP.api('/themes?name=' + encodeURIComponent(theme.name), { method: 'DELETE' });
              HP.toast('success', t('themes.removed', { name: theme.name }, '{name} removed'));
              await load();
            } catch (e) { HP.notifyError(e); }
          }
        });
      }
      async function install() {
        if (!installForm.url.trim()) { HP.toast('error', t('themes.url_required', {}, 'Enter a Git repository URL')); return; }
        installing.value = true;
        try {
          await HP.api('/themes/install', { method: 'POST', body: { url: installForm.url.trim(), name: installForm.name.trim() } });
          installVisible.value = false;
          installForm.url = '';
          installForm.name = '';
          HP.toast('success', t('themes.installed', {}, 'Theme installed'));
          await load();
        } catch (e) { HP.notifyError(e); }
        finally { installing.value = false; }
      }
      function screenshot(theme) { return '/api/themes/screenshot?name=' + encodeURIComponent(theme.name); }
      onMounted(load);
      return {
        tab, themes, active, loading, error, installing, installVisible, installForm, busy, load,
        activate, update, remove, install, screenshot, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('themes.design', {}, 'Design')" :title="t('themes.title', {}, 'Themes')" :subtitle="t('themes.subtitle', {}, 'Choose a look, install themes and tune the details.')">
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="load" :title="t('common.refresh')" />
            <PButton :label="t('themes.install', {}, 'Install theme')" icon="pi pi-download" @click="installVisible = true" class="btn-primary" />
          </template>
        </PageHeader>

        <PTabs v-model:value="tab">
          <PTabList>
            <PTab value="installed"><i class="pi pi-palette"></i> {{ t('themes.installed_tab', {}, 'Installed themes') }}</PTab>
            <PTab value="config"><i class="pi pi-sliders-h"></i> {{ t('themes.config_tab', {}, 'Theme settings') }}</PTab>
          </PTabList>
          <PTabPanels>
            <PTabPanel value="installed">
              <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
              <div v-if="loading" class="themes-grid">
                <PCard v-for="n in 3" :key="n" class="theme-card"><template #content><PSkeleton height="280px" /></template></PCard>
              </div>
              <EmptyState v-else-if="themes.length === 0" icon="pi pi-palette" :title="t('themes.empty_title', {}, 'No themes installed')" :text="t('themes.empty_text', {}, 'Clone a Hugo theme from Git to get started.')">
                <PButton :label="t('themes.install', {}, 'Install theme')" icon="pi pi-download" @click="installVisible = true" />
              </EmptyState>
              <div v-else class="themes-grid">
                <PCard v-for="theme in themes" :key="theme.name" class="theme-card" :class="{ active: theme.active }">
                  <template #content>
                    <div class="theme-preview">
                      <div class="theme-placeholder"><i class="pi pi-palette"></i><span>{{ (theme.name || 'T')[0].toUpperCase() }}</span></div>
                      <img v-if="theme.screenshot" :src="screenshot(theme)" :alt="theme.name" loading="lazy" @error="$event.target.remove()" />
                      <PTag v-if="theme.active" class="theme-active-tag" :value="t('themes.active', {}, 'Active')" severity="success" rounded />
                    </div>
                    <div class="theme-body">
                      <div class="theme-title-row"><h3>{{ theme.title || theme.name }}</h3><PTag v-if="theme.has_git" value="Git" severity="secondary" /></div>
                      <p class="theme-path">{{ theme.name }}</p>
                      <p class="theme-desc">{{ theme.description || t('themes.no_description', {}, 'No description provided by this theme.') }}</p>
                      <div class="theme-tags"><PTag v-for="tag in theme.tags.slice(0, 3)" :key="tag" :value="tag" severity="secondary" rounded /></div>
                      <div class="theme-meta"><span v-if="theme.author"><i class="pi pi-user"></i>{{ theme.author }}</span><span v-if="theme.license"><i class="pi pi-file"></i>{{ theme.license }}</span></div>
                      <div class="theme-actions">
                        <PButton v-if="!theme.active" size="small" :label="t('themes.activate', {}, 'Activate')" :loading="busy[theme.name]" @click="activate(theme)" class="btn-primary" />
                        <PButton v-else size="small" outlined :label="t('themes.active', {}, 'Active')" disabled />
                        <PButton v-if="theme.has_git" size="small" text icon="pi pi-refresh" :label="t('themes.update', {}, 'Update')" :loading="busy[theme.name]" @click="update(theme)" />
                        <PButton v-if="!theme.active" size="small" text severity="danger" icon="pi pi-trash" @click="remove(theme)" />
                        <a v-if="theme.homepage" :href="theme.homepage" target="_blank" rel="noopener" class="theme-link"><i class="pi pi-external-link"></i></a>
                      </div>
                    </div>
                  </template>
                </PCard>
              </div>
            </PTabPanel>
            <PTabPanel value="config">
              <SiteConfigPanel @saved="load" />
            </PTabPanel>
          </PTabPanels>
        </PTabs>

        <PDialog v-model:visible="installVisible" modal :header="t('themes.install', {}, 'Install theme')" :style="{ width: '32rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <PMessage severity="info" :closable="false">{{ t('themes.install_hint', {}, 'The repository is cloned into themes/ with Git. Private repositories need Git credentials mounted or configured on the server.') }}</PMessage>
            <label class="field"><span>{{ t('themes.repo_url', {}, 'Git repository URL') }}</span><PInputText v-model="installForm.url" placeholder="https://github.com/user/theme.git" class="mono-input" /></label>
            <label class="field"><span>{{ t('themes.folder_name', {}, 'Folder name') }} <em>{{ t('common.optional', {}, 'optional') }}</em></span><PInputText v-model="installForm.name" placeholder="theme" /></label>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="installVisible = false" />
            <PButton :label="t('themes.install', {}, 'Install theme')" icon="pi pi-download" :loading="installing" @click="install" />
          </template>
        </PDialog>
      </div>
    `
  };
})();
