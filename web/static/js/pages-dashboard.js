(function () {
  'use strict';
  const HP = window.HP;
  const { ref, reactive, computed, onMounted } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  components.DashboardPage = {
    name: 'DashboardPage',
    setup() {
      const state = HP.state;
      const data = ref(null);
      const loading = ref(true);
      const error = ref('');
      const building = ref(false);
      const buildResult = ref(null);

      const maxActivity = computed(() => {
        if (!data.value) return 1;
        return Math.max(1, ...data.value.activity.map((m) => m.count));
      });
      const publishedPct = computed(() => {
        if (!data.value || !data.value.content.total) return 0;
        return Math.round((data.value.content.published / data.value.content.total) * 100);
      });
      const draftPct = computed(() => {
        if (!data.value || !data.value.content.total) return 0;
        return Math.round((data.value.content.drafts / data.value.content.total) * 100);
      });
      const currentBuild = computed(() => buildResult.value || (data.value && data.value.build) || null);
      const siteTitle = computed(() => (data.value && data.value.site.title) || t('dashboard.default_site', {}, 'Your Hugo site'));
      const stats = computed(() => {
        if (!data.value) return [];
        return [
          { key: 'total', label: t('dashboard.total_content', {}, 'Total content'), value: data.value.content.total, icon: 'pi pi-file-edit', hint: t('dashboard.words_count', { count: HP.formatNumber(data.value.content.words) }, '{count} words') },
          { key: 'published', label: t('dashboard.published', {}, 'Published'), value: data.value.content.published, icon: 'pi pi-check-circle', hint: t('dashboard.published_hint', { count: data.value.content.scheduled }, '{count} scheduled') },
          { key: 'drafts', label: t('dashboard.drafts', {}, 'Drafts'), value: data.value.content.drafts, icon: 'pi pi-pencil', hint: t('dashboard.drafts_hint', { count: data.value.content.errors }, '{count} with errors') },
          { key: 'themes', label: t('dashboard.themes', {}, 'Themes'), value: data.value.themes.total, icon: 'pi pi-palette', hint: data.value.themes.active || t('dashboard.no_theme', {}, 'No active theme') }
        ];
      });

      async function load() {
        loading.value = true;
        error.value = '';
        try {
          data.value = await HP.api('/dashboard');
        } catch (e) {
          error.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }
      async function buildNow() {
        if (building.value) return;
        building.value = true;
        try {
          const result = await HP.api('/build', { method: 'POST' });
          buildResult.value = result;
          if (result.success) HP.toast('success', t('build.success', {}, 'Site built successfully'), t('build.duration', { duration: (result.duration_ms / 1000).toFixed(1) + 's' }, 'Finished in {duration}'));
          else HP.toast('error', t('build.failed', {}, 'Build failed'), t('build.open_output', {}, 'Open Build to review the output.'));
          if (data.value) data.value.build = result;
        } catch (e) {
          HP.notifyError(e);
        } finally {
          building.value = false;
        }
      }
      function edit(item) { HP.navigate('/content/edit', { path: item.path }); }
      function barHeight(count) {
        return Math.max(count > 0 ? 6 : 2, Math.round((count / maxActivity.value) * 100)) + '%';
      }
      onMounted(load);
      return {
        state, data, loading, error, building, buildResult, stats, maxActivity, publishedPct,
        draftPct, currentBuild, siteTitle, load, buildNow, edit, barHeight,
        go: HP.navigate, formatDate: HP.formatDate, formatDateTime: HP.formatDateTime,
        formatMonth: HP.formatMonth, formatBytes: HP.formatBytes, formatNumber: HP.formatNumber, safeUrl: HP.safeUrl, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('dashboard.overview', {}, 'Overview')" :title="t('dashboard.title', {}, 'Dashboard')" :subtitle="siteTitle" live>
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="load" :title="t('common.refresh')" />
            <PButton outlined :label="t('content.new', {}, 'New content')" icon="pi pi-plus" @click="go('/content/new')" />
            <PButton :label="t('build.build_now', {}, 'Build site')" icon="pi pi-cloud-upload" :loading="building" @click="buildNow" class="btn-primary" />
          </template>
        </PageHeader>

        <div v-if="loading" class="stats-grid">
          <PCard v-for="n in 4" :key="n" class="stat-card"><template #content><PSkeleton height="84px" /></template></PCard>
        </div>
        <PMessage v-else-if="error" severity="error" :closable="false">
          {{ error }} <button class="link-button" @click="load">{{ t('common.retry', {}, 'Retry') }}</button>
        </PMessage>

        <template v-else-if="data">
          <PMessage v-if="!data.site.initialized" severity="warn" :closable="false">
            {{ t('themes.not_initialized', {}, 'This folder does not contain a Hugo configuration yet.') }}
            <button class="link-button" @click="go('/themes', { tab: 'config' })">{{ t('themes.init_site', {}, 'Initialize site') }}</button>
          </PMessage>
          <div class="stats-grid">
            <PCard v-for="stat in stats" :key="stat.key" class="stat-card">
              <template #content>
                <div class="stat-top"><span class="stat-label">{{ stat.label }}</span><span class="stat-icon" :class="'tone-' + stat.key"><i :class="stat.icon"></i></span></div>
                <div class="stat-value">{{ formatNumber(stat.value) }}</div>
                <div class="stat-hint">{{ stat.hint }}</div>
              </template>
            </PCard>
          </div>

          <div class="dashboard-grid">
            <div class="dashboard-main">
              <PCard class="panel-card chart-card">
                <template #content>
                  <div class="panel-head">
                    <div><h2>{{ t('dashboard.content_activity', {}, 'Content activity') }}</h2><p>{{ t('dashboard.content_activity_hint', {}, 'Publishing rhythm over the last six months') }}</p></div>
                    <span class="legend"><span class="legend-dot"></span>{{ t('dashboard.published', {}, 'Published') }}</span>
                  </div>
                  <div class="bar-chart">
                    <div class="chart-yard"><span v-for="n in 4" :key="n"></span></div>
                    <div v-for="m in data.activity" :key="m.month" class="chart-col" :title="m.month + ': ' + m.count">
                      <div class="chart-bar-wrap"><div class="chart-bar" :style="{ height: barHeight(m.count) }"><span>{{ m.count }}</span></div></div>
                      <span class="chart-label">{{ formatMonth(m.month) }}</span>
                    </div>
                  </div>
                  <div v-if="data.content.total === 0" class="chart-empty">{{ t('dashboard.no_activity', {}, 'No content yet — your first post will appear here.') }}</div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head">
                    <div><h2>{{ t('dashboard.recent_content', {}, 'Recent content') }}</h2><p>{{ t('dashboard.recent_hint', {}, 'Jump back into what you were working on') }}</p></div>
                    <PButton text :label="t('common.view_all', {}, 'View all')" icon="pi pi-arrow-right" iconPos="right" @click="go('/content')" />
                  </div>
                  <div v-if="data.recent.length === 0" class="empty-inline">
                    <i class="pi pi-file-edit"></i>
                    <span>{{ t('dashboard.no_content', {}, 'No content yet. Create your first page.') }}</span>
                    <PButton :label="t('content.new', {}, 'New content')" icon="pi pi-plus" size="small" @click="go('/content/new')" />
                  </div>
                  <PDataTable v-else :value="data.recent" dataKey="path" class="dash-table" responsiveLayout="scroll">
                    <PColumn field="title" :header="t('content.title_field', {}, 'Title')">
                      <template #body="{ data: row }">
                        <button class="table-title" @click="edit(row)">{{ row.title }}</button>
                        <div class="table-sub">{{ row.path }}</div>
                      </template>
                    </PColumn>
                    <PColumn field="section" :header="t('content.section', {}, 'Section')">
                      <template #body="{ data: row }"><span class="muted">{{ row.section || t('content.root', {}, 'Root') }}</span></template>
                    </PColumn>
                    <PColumn field="status" :header="t('common.status', {}, 'Status')">
                      <template #body="{ data: row }"><StatusTag :status="row.status" /></template>
                    </PColumn>
                    <PColumn field="date" :header="t('content.date', {}, 'Date')">
                      <template #body="{ data: row }"><span class="muted nowrap">{{ formatDate(row.date || row.mod_time) }}</span></template>
                    </PColumn>
                  </PDataTable>
                </template>
              </PCard>
            </div>

            <div class="dashboard-side">
              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('dashboard.site_overview', {}, 'Site overview') }}</h2><p>{{ t('dashboard.site_overview_hint', {}, 'Configuration at a glance') }}</p></div></div>
                  <div class="site-title-row">
                    <span class="site-avatar"><i class="pi pi-globe"></i></span>
                    <div><strong>{{ siteTitle }}</strong><a v-if="safeUrl(data.site.base_url)" :href="safeUrl(data.site.base_url)" target="_blank" rel="noopener">{{ safeUrl(data.site.base_url).replace(/^https?:\\/\\//, '') }}</a><span v-else class="muted">{{ t('dashboard.no_base_url', {}, 'No base URL set') }}</span></div>
                  </div>
                  <dl class="key-list">
                    <div><dt>{{ t('dashboard.active_theme', {}, 'Active theme') }}</dt><dd>{{ data.themes.active || '—' }}</dd></div>
                    <div><dt>{{ t('dashboard.hugo_version', {}, 'Hugo') }}</dt><dd>{{ data.hugo.available ? data.hugo.version : t('dashboard.hugo_missing', {}, 'Not found') }} <PTag v-if="data.hugo.outdated" severity="warn" :value="t('dashboard.hugo_update', {}, 'Update available')" /></dd></div>
                    <div><dt>{{ t('dashboard.media_files', {}, 'Media') }}</dt><dd>{{ formatNumber(data.media.files) }} · {{ formatBytes(data.media.bytes) }}</dd></div>
                  </dl>
                  <div class="progress-block">
                    <div class="progress-label"><span>{{ t('dashboard.published', {}, 'Published') }}</span><strong>{{ publishedPct }}%</strong></div>
                    <div class="progress-track"><div class="progress-fill success" :style="{ width: publishedPct + '%' }"></div></div>
                    <div class="progress-label"><span>{{ t('dashboard.drafts', {}, 'Drafts') }}</span><strong>{{ draftPct }}%</strong></div>
                    <div class="progress-track"><div class="progress-fill warn" :style="{ width: draftPct + '%' }"></div></div>
                  </div>
                  <PButton outlined class="w-full" :label="t('nav.themes', {}, 'Themes')" icon="pi pi-palette" @click="go('/themes', { tab: 'config' })" />
                </template>
              </PCard>

              <PCard class="panel-card build-mini">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('dashboard.last_build', {}, 'Last build') }}</h2><p>{{ t('dashboard.build_hint', {}, 'Static output status') }}</p></div><StatusTag :status="currentBuild ? currentBuild.status : 'idle'" /></div>
                  <div class="build-meta">
                    <span>{{ currentBuild ? formatDateTime(currentBuild.finished_at || currentBuild.started_at) : t('dashboard.never_built', {}, 'Never built') }}</span>
                    <span v-if="currentBuild && currentBuild.duration_ms">{{ (currentBuild.duration_ms / 1000).toFixed(1) }}s</span>
                  </div>
                  <PButton text class="w-full" :label="t('build.open', {}, 'Open build console')" icon="pi pi-arrow-right" iconPos="right" @click="go('/build')" />
                </template>
              </PCard>
            </div>
          </div>
        </template>
      </div>
    `
  };
})();
