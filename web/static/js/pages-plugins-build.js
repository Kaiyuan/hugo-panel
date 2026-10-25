(function () {
  'use strict';
  const HP = window.HP;
  const { ref, reactive, computed, onMounted } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  components.PluginsPage = {
    name: 'PluginsPage',
    setup() {
      const confirm = PrimeVue.useConfirm();
      const state = ref(null);
      const loading = ref(true);
      const error = ref('');
      const addVisible = ref(false);
      const adding = ref(false);
      const form = reactive({ path: '', version: '' });
      const output = ref('');
      const showOutput = ref(false);
      const tidyRunning = ref(false);
      const removing = ref('');

      const modules = computed(() => (state.value && state.value.modules) || []);
      const toolchain = computed(() => {
        if (!state.value) return [];
        return [
          { key: 'hugo', label: 'Hugo', ok: state.value.hugo_available, value: state.value.hugo_version || '' },
          { key: 'go', label: 'Go', ok: state.value.go_available, value: '' },
          { key: 'git', label: 'Git', ok: state.value.git_available, value: '' }
        ];
      });
      function apply(result) {
        if (!result) return;
        state.value = {
          modules: result.modules || [],
          hugo_available: result.hugo_available,
          go_available: result.go_available,
          git_available: result.git_available,
          hugo_version: result.hugo_version,
          config_path: result.config_path,
          initialized: result.initialized
        };
        if (result.output) { output.value = result.output; showOutput.value = true; }
        if (result.warning) HP.toast('warn', t('plugins.warning', {}, 'Completed with warnings'), result.warning, 9000);
      }
      async function load() {
        loading.value = true;
        error.value = '';
        try {
          state.value = await HP.api('/plugins');
        } catch (e) { error.value = HP.errorText(e); }
        finally { loading.value = false; }
      }
      async function add() {
        if (!form.path.trim()) { HP.toast('error', t('plugins.path_required', {}, 'Enter a module path')); return; }
        adding.value = true;
        try {
          const result = await HP.api('/plugins', { method: 'POST', body: { path: form.path.trim(), version: form.version.trim() } });
          apply(result);
          addVisible.value = false;
          form.path = '';
          form.version = '';
          HP.toast('success', t('plugins.added', {}, 'Plugin added'));
        } catch (e) { HP.notifyError(e); }
        finally { adding.value = false; }
      }
      function remove(mod) {
        confirm.require({
          message: t('plugins.remove_confirm', { path: mod.path }, 'Remove “{path}” from Hugo modules?'),
          header: t('plugins.remove', {}, 'Remove plugin'),
          icon: 'pi pi-trash', acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'), rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            removing.value = mod.path;
            try {
              const result = await HP.api('/plugins?path=' + encodeURIComponent(mod.path), { method: 'DELETE' });
              apply(result);
              HP.toast('success', t('plugins.removed', {}, 'Plugin removed'));
            } catch (e) { HP.notifyError(e); }
            finally { removing.value = ''; }
          }
        });
      }
      async function tidy() {
        tidyRunning.value = true;
        try {
          const result = await HP.api('/plugins/tidy', { method: 'POST' });
          apply(result);
          HP.toast('success', t('plugins.tidy_done', {}, 'Modules tidied'));
        } catch (e) { HP.notifyError(e); }
        finally { tidyRunning.value = false; }
      }
      onMounted(load);
      return {
        state, loading, error, addVisible, adding, form, output, showOutput, tidyRunning, removing,
        modules, toolchain, load, add, remove, tidy, go: HP.navigate, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('plugins.extensions', {}, 'Extensions')" :title="t('plugins.title', {}, 'Plugins')" :subtitle="t('plugins.subtitle', {}, 'Manage Hugo modules as installable plugins.')">
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="load" :title="t('common.refresh')" />
            <PButton text :label="t('plugins.tidy', {}, 'Tidy modules')" icon="pi pi-sparkles" :loading="tidyRunning" @click="tidy" />
            <PButton :label="t('plugins.add', {}, 'Add plugin')" icon="pi pi-plus" @click="addVisible = true" class="btn-primary" />
          </template>
        </PageHeader>

        <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>

        <div v-if="loading" class="editor-loading"><PProgressSpinner strokeWidth="3" /><span>{{ t('common.loading', {}, 'Loading…') }}</span></div>
        <template v-else-if="state">
          <div class="requirement-bar">
            <span class="requirement-label">{{ t('plugins.toolchain', {}, 'Toolchain') }}</span>
            <span v-for="item in toolchain" :key="item.key" class="toolchain-badge" :class="{ ok: item.ok, missing: !item.ok }">
              <i :class="item.ok ? 'pi pi-check-circle' : 'pi pi-exclamation-circle'"></i>
              <span>{{ item.label }}</span>
              <small v-if="item.value">{{ item.value.replace('hugo ', '') }}</small>
            </span>
          </div>

          <PMessage v-if="!state.initialized" severity="warn" :closable="false">
            {{ t('plugins.not_initialized', {}, 'Initialize your Hugo site before adding modules.') }}
            <button class="link-button" @click="go('/themes', { tab: 'config' })">{{ t('plugins.open_settings', {}, 'Open theme settings') }}</button>
          </PMessage>
          <PMessage v-else-if="!state.go_available" severity="warn" :closable="false">
            {{ t('plugins.go_missing', {}, 'Hugo modules need the Go toolchain on the server. Install Go or manage modules outside the panel.') }}
          </PMessage>

          <div v-if="modules.length === 0" class="empty-state panel-card">
            <span class="empty-icon"><i class="pi pi-box"></i></span>
            <h3>{{ t('plugins.empty_title', {}, 'No modules installed') }}</h3>
            <p>{{ t('plugins.empty_text', {}, 'Hugo modules add shortcodes, assets and integrations without editing theme code.') }}</p>
            <div class="empty-actions"><PButton :label="t('plugins.add', {}, 'Add plugin')" icon="pi pi-plus" @click="addVisible = true" /></div>
          </div>
          <div v-else class="plugins-grid">
            <PCard v-for="mod in modules" :key="mod.path" class="panel-card plugin-card">
              <template #content>
                <div class="plugin-head">
                  <span class="module-icon"><i class="pi pi-box"></i></span>
                  <div class="plugin-title"><strong>{{ mod.path }}</strong><small>{{ t('plugins.hugo_module', {}, 'Hugo module') }}</small></div>
                  <PTag v-if="mod.disable" :value="t('common.disabled', {}, 'Disabled')" severity="secondary" />
                  <PButton text rounded severity="danger" icon="pi pi-trash" :loading="removing === mod.path" @click="remove(mod)" :title="t('common.delete')" />
                </div>
                <div class="plugin-details">
                  <span><i class="pi pi-database"></i>{{ t('plugins.config_import', {}, 'config module import') }}</span>
                  <span v-if="mod.ignore_config"><i class="pi pi-eye-slash"></i>{{ t('plugins.ignore_config', {}, 'Ignore config') }}</span>
                  <span v-if="mod.no_vendor"><i class="pi pi-ban"></i>{{ t('plugins.no_vendor', {}, 'No vendor') }}</span>
                </div>
              </template>
            </PCard>
          </div>

          <a href="https://gohugo.io/hugo-modules/" target="_blank" rel="noopener" class="docs-link">
            <i class="pi pi-external-link"></i>{{ t('plugins.docs', {}, 'Read the Hugo modules documentation') }}
          </a>

          <PCard v-if="output" class="panel-card output-card">
            <template #content>
              <div class="panel-head"><div><h2>{{ t('plugins.output', {}, 'Last operation output') }}</h2><p>{{ t('plugins.output_hint', {}, 'Normalised output from hugo mod commands') }}</p></div><PButton text :label="showOutput ? t('common.hide', {}, 'Hide') : t('common.show', {}, 'Show')" @click="showOutput = !showOutput" /></div>
              <pre v-if="showOutput" class="console-output">{{ output }}</pre>
            </template>
          </PCard>
        </template>

        <PDialog v-model:visible="addVisible" modal :header="t('plugins.add', {}, 'Add plugin')" :style="{ width: '32rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <PMessage severity="info" :closable="false">{{ t('plugins.add_hint', {}, 'Enter a Go module path. The panel adds it to module.imports and runs hugo mod tidy.') }}</PMessage>
            <label class="field"><span>{{ t('plugins.module_path', {}, 'Module path') }}</span><PInputText v-model="form.path" placeholder="github.com/user/hugo-module" class="mono-input" /></label>
            <label class="field"><span>{{ t('plugins.version', {}, 'Version') }} <em>{{ t('common.optional', {}, 'optional') }}</em></span><PInputText v-model="form.version" placeholder="v1.2.3" class="mono-input" /></label>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="addVisible = false" />
            <PButton :label="t('plugins.add', {}, 'Add plugin')" icon="pi pi-plus" :loading="adding" @click="add" />
          </template>
        </PDialog>
      </div>
    `
  };

  components.BuildPage = {
    name: 'BuildPage',
    setup() {
      const build = ref(null);
      const loading = ref(true);
      const building = ref(false);
      const error = ref('');
      const siteBase = ref('');
      const configExists = ref(true);

      async function load() {
        loading.value = true;
        error.value = '';
        try {
          const [data, cfg] = await Promise.all([
            HP.api('/build'),
            HP.api('/hugo/config').catch(() => null)
          ]);
          build.value = data.build;
          if (cfg) { siteBase.value = cfg.base_url || ''; configExists.value = !!cfg.exists; }
        } catch (e) { error.value = HP.errorText(e); }
        finally { loading.value = false; }
      }
      async function run() {
        building.value = true;
        error.value = '';
        try {
          const result = await HP.api('/build', { method: 'POST' });
          build.value = result;
          if (result.success) HP.toast('success', t('build.success', {}, 'Site built successfully'));
          else HP.toast('error', t('build.failed', {}, 'Build failed'));
        } catch (e) { HP.notifyError(e); }
        finally { building.value = false; }
      }
      const status = computed(() => build.value ? build.value.status : 'idle');
      const duration = computed(() => build.value && build.value.duration_ms ? (build.value.duration_ms / 1000).toFixed(1) + 's' : '—');
      function copyOutput() { if (build.value) HP.copyText(build.value.output || ''); }
      onMounted(load);
      return {
        build, loading, building, error, siteBase, configExists, status, duration, load, run, copyOutput, safeUrl: HP.safeUrl,
        go: HP.navigate, formatDateTime: HP.formatDateTime, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('build.deploy', {}, 'Deploy')" :title="t('build.title', {}, 'Build')" :subtitle="t('build.subtitle', {}, 'Generate the static site with Hugo and inspect the output.')">
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="load" :title="t('common.refresh')" />
            <a v-if="safeUrl(siteBase)" :href="safeUrl(siteBase)" target="_blank" rel="noopener" class="p-button p-button-outlined p-button-sm open-site-btn"><i class="pi pi-external-link"></i> {{ t('build.open_site', {}, 'Open site') }}</a>
            <PButton :label="t('build.build_now', {}, 'Build site')" icon="pi pi-play" :loading="building" @click="run" class="btn-primary" />
          </template>
        </PageHeader>

        <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
        <PMessage v-if="!configExists && !loading" severity="warn" :closable="false">
          {{ t('build.not_initialized', {}, 'No Hugo configuration found. Initialize the site first.') }}
          <button class="link-button" @click="go('/themes', { tab: 'config' })">{{ t('nav.themes', {}, 'Themes') }}</button>
        </PMessage>

        <div v-if="loading" class="editor-loading"><PProgressSpinner strokeWidth="3" /><span>{{ t('common.loading', {}, 'Loading…') }}</span></div>
        <template v-else>
          <PCard class="panel-card build-summary">
            <template #content>
              <div class="build-hero">
                <span class="build-icon" :class="status"><i :class="status === 'success' ? 'pi pi-check' : (status === 'failed' ? 'pi pi-times' : 'pi pi-bolt')"></i></span>
                <div class="build-copy">
                  <div class="build-status-line"><h2>{{ build ? t('build.result_' + status, {}, status) : t('build.never_title', {}, 'No builds yet') }}</h2><StatusTag :status="status" /></div>
                  <p v-if="build">{{ formatDateTime(build.finished_at || build.started_at) }} · {{ t('build.duration', { duration }, 'Finished in {duration}') }} · {{ t('build.exit_code', { code: build.exit_code }, 'Exit code {code}') }}</p>
                  <p v-else>{{ t('build.never_text', {}, 'Run your first build to generate the public folder.') }}</p>
                </div>
                <PButton :label="building ? t('build.building', {}, 'Building…') : t('build.build_now', {}, 'Build site')" icon="pi pi-play" :loading="building" @click="run" />
              </div>
              <div v-if="build" class="build-facts">
                <div><span>{{ t('build.command', {}, 'Command') }}</span><code>{{ build.command }}</code></div>
                <div><span>{{ t('build.started', {}, 'Started') }}</span><strong>{{ formatDateTime(build.started_at) }}</strong></div>
                <div><span>{{ t('build.finished', {}, 'Finished') }}</span><strong>{{ formatDateTime(build.finished_at) }}</strong></div>
                <div><span>{{ t('build.duration_label', {}, 'Duration') }}</span><strong>{{ duration }}</strong></div>
              </div>
            </template>
          </PCard>

          <PCard class="panel-card output-card">
            <template #content>
              <div class="panel-head">
                <div><h2>{{ t('build.output', {}, 'Build output') }}</h2><p>{{ t('build.output_hint', {}, 'Combined stdout and stderr from Hugo') }}</p></div>
                <PButton v-if="build" text icon="pi pi-copy" :label="t('common.copy', {}, 'Copy')" @click="copyOutput" />
              </div>
              <pre v-if="build" class="console-output large">{{ build.output || t('build.no_output', {}, 'No output.') }}</pre>
              <div v-else class="empty-inline"><i class="pi pi-terminal"></i><span>{{ t('build.no_build_yet', {}, 'Build output will appear here.') }}</span></div>
            </template>
          </PCard>
        </template>
      </div>
    `
  };
})();
