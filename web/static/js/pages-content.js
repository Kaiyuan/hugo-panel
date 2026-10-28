(function () {
  'use strict';
  const HP = window.HP;
  const { ref, reactive, computed, onMounted, watch, onBeforeUnmount, nextTick } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  components.ContentPage = {
    name: 'ContentPage',
    setup() {
      const route = HP.route;
      const confirm = PrimeVue.useConfirm();
      const list = ref({ items: [], total: 0, page: 1, limit: 20 });
      const meta = ref({ sections: [], tags: [], categories: [], exists: false });
      const filters = reactive({
        q: route.query.q || '',
        section: route.query.section || '',
        status: ''
      });
      const page = ref(1);
      const loading = ref(true);
      const metaLoading = ref(true);
      const error = ref('');

      const sectionOptions = computed(() => {
        const options = (meta.value.sections || []).map((s) => ({
          name: s.name,
          label: s.name === 'root' ? t('content.root', {}, 'Root') : s.name,
          count: s.count
        }));
        return [{ name: '', label: t('content.all_sections', {}, 'All sections') }].concat(options);
      });
      const statusOptions = computed(() => [
        { value: '', label: t('content.all_statuses', {}, 'All statuses') },
        { value: 'published', label: t('status.published', {}, 'Published') },
        { value: 'draft', label: t('status.draft', {}, 'Draft') },
        { value: 'scheduled', label: t('status.scheduled', {}, 'Scheduled') },
        { value: 'error', label: t('status.error', {}, 'Error') }
      ]);
      const totalPages = computed(() => Math.max(1, Math.ceil(list.value.total / list.value.limit)));
      const pageInfo = computed(() => {
        if (!list.value.total) return t('content.no_results', {}, 'No content found');
        const start = (list.value.page - 1) * list.value.limit + 1;
        const end = Math.min(list.value.page * list.value.limit, list.value.total);
        return t('content.showing', { start, end, total: list.value.total }, 'Showing {start}–{end} of {total}');
      });

      async function loadMeta() {
        metaLoading.value = true;
        try { meta.value = await HP.api('/content/meta'); }
        catch (e) { /* non-fatal */ }
        finally { metaLoading.value = false; }
      }
      async function load() {
        loading.value = true;
        error.value = '';
        const params = new URLSearchParams();
        params.set('page', String(page.value));
        params.set('limit', '20');
        if (filters.q) params.set('q', filters.q);
        if (filters.section) params.set('section', filters.section);
        if (filters.status) params.set('status', filters.status);
        try {
          list.value = await HP.api('/content?' + params.toString());
        } catch (e) {
          error.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }
      const debouncedSearch = HP.debounce(() => { page.value = 1; load(); }, 350);
      watch(() => filters.q, () => debouncedSearch());
      watch(() => [filters.section, filters.status], () => { page.value = 1; load(); });
      function clearFilters() {
        filters.q = '';
        filters.section = '';
        filters.status = '';
        page.value = 1;
        load();
      }
      function refresh() { loadMeta(); load(); }
      function edit(item) { HP.navigate('/content/edit', { path: item.path }); }
      function openNew() {
        HP.navigate('/content/new', filters.section ? { section: filters.section } : {});
      }
      function remove(item) {
        confirm.require({
          message: t('content.delete_confirm', { title: item.title }, 'Delete “{title}”? This cannot be undone.'),
          header: t('content.delete', {}, 'Delete content'),
          icon: 'pi pi-trash',
          acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'),
          rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            try {
              await HP.api('/content?path=' + encodeURIComponent(item.path), { method: 'DELETE' });
              HP.toast('success', t('content.deleted', {}, 'Content deleted'));
              if (list.value.items.length === 1 && page.value > 1) page.value--;
              refresh();
            } catch (e) { HP.notifyError(e); }
          }
        });
      }
      function setPage(next) {
        if (next < 1 || next > totalPages.value || next === page.value) return;
        page.value = next;
        load();
      }
      onMounted(() => { loadMeta(); load(); });
      return {
        list, meta, filters, page, loading, metaLoading, error, sectionOptions, statusOptions,
        totalPages, pageInfo, load, loadMeta, refresh, edit, openNew, remove, clearFilters, setPage,
        go: HP.navigate, formatDate: HP.formatDate, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('content.library', {}, 'Library')" :title="t('content.title', {}, 'Content')" :subtitle="t('content.subtitle', {}, 'Write, edit and publish Markdown pages.')">
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="refresh" :title="t('common.refresh')" />
            <PButton :label="t('content.new', {}, 'New content')" icon="pi pi-plus" @click="openNew" class="btn-primary" />
          </template>
        </PageHeader>

        <PMessage v-if="!metaLoading && meta.initialized === false" severity="warn" :closable="false">
          {{ t('themes.not_initialized', {}, 'This folder does not contain a Hugo configuration yet.') }}
          <button class="link-button" @click="go('/themes', { tab: 'config' })">{{ t('themes.init_site', {}, 'Initialize site') }}</button>
        </PMessage>

        <div class="toolbar-card">
          <div class="toolbar-row">
            <div class="search-box wide">
              <i class="pi pi-search"></i>
              <input class="search-input" v-model="filters.q" :placeholder="t('content.search_placeholder', {}, 'Search content…')" />
            </div>
            <PSelect v-model="filters.section" :options="sectionOptions" optionLabel="label" optionValue="name" class="filter-select" :aria-label="t('content.section')" />
            <PSelect v-model="filters.status" :options="statusOptions" optionLabel="label" optionValue="value" class="filter-select" :aria-label="t('common.status')" />
            <PButton text icon="pi pi-filter-slash" @click="clearFilters" :title="t('content.clear_filters', {}, 'Clear filters')" />
          </div>
        </div>

        <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>

        <PCard class="panel-card table-card">
          <template #content>
            <PDataTable :value="list.items" :loading="loading" dataKey="path" class="content-table" responsiveLayout="scroll">
              <PColumn field="title" :header="t('content.title_field', {}, 'Title')">
                <template #body="{ data: row }">
                  <div class="row-title">
                    <span class="file-icon"><i :class="row.bundle ? 'pi pi-box' : 'pi pi-file'"></i></span>
                    <div>
                      <button class="table-title" @click="edit(row)">{{ row.title }}</button>
                      <div class="table-sub">{{ row.path }}</div>
                    </div>
                  </div>
                </template>
              </PColumn>
              <PColumn field="section" :header="t('content.section', {}, 'Section')">
                <template #body="{ data: row }"><span class="chip-muted">{{ row.section || t('content.root', {}, 'Root') }}</span></template>
              </PColumn>
              <PColumn field="status" :header="t('common.status', {}, 'Status')">
                <template #body="{ data: row }"><StatusTag :status="row.status" /></template>
              </PColumn>
              <PColumn field="date" :header="t('content.date', {}, 'Date')">
                <template #body="{ data: row }"><span class="muted nowrap">{{ formatDate(row.date || row.mod_time) }}</span></template>
              </PColumn>
              <PColumn field="word_count" :header="t('content.words', {}, 'Words')">
                <template #body="{ data: row }"><span class="muted">{{ row.word_count }}</span></template>
              </PColumn>
              <PColumn :header="''" style="width: 96px">
                <template #body="{ data: row }">
                  <div class="row-actions">
                    <PButton text rounded icon="pi pi-pencil" @click.stop="edit(row)" :title="t('common.edit')" />
                    <PButton text rounded severity="danger" icon="pi pi-trash" @click.stop="remove(row)" :title="t('common.delete')" />
                  </div>
                </template>
              </PColumn>
              <template #empty>
                <EmptyState icon="pi pi-file-edit" :title="t('content.empty_title', {}, 'No content yet')" :text="t('content.empty_text', {}, 'Create your first Markdown page and start publishing.')">
                  <PButton :label="t('content.new', {}, 'New content')" icon="pi pi-plus" @click="openNew" />
                </EmptyState>
              </template>
            </PDataTable>
            <div class="table-footer">
              <span class="muted">{{ pageInfo }}</span>
              <div class="pager">
                <PButton text rounded icon="pi pi-angle-left" :disabled="page <= 1" @click="setPage(page - 1)" :aria-label="t('common.previous')" />
                <span class="pager-current">{{ page }} / {{ totalPages }}</span>
                <PButton text rounded icon="pi pi-angle-right" :disabled="page >= totalPages" @click="setPage(page + 1)" :aria-label="t('common.next')" />
              </div>
            </div>
          </template>
        </PCard>
      </div>
    `
  };

  const KNOWN_FIELDS = ['title', 'slug', 'date', 'draft', 'description', 'tags', 'categories', 'weight', 'url', 'author', 'type', 'image'];

  function escapeHighlightHtml(value) {
    return String(value).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  // Line-based Markdown highlighter for the source editor overlay. It escapes
  // all text before adding spans, so the rendered HTML is always safe.
  function highlightMarkdownInline(raw) {
    const pattern = /(`[^`\n]+`)|(!\[[^\]\n]*\]\([^)\n]*\))|(\[[^\]\n]*\]\([^)\n]*\))|(\*\*[^*\n]+\*\*|__[^_\n]+__)|(~~[^~\n]+~~)|(\*[^*\n]+\*|_[^_\n]+_)/g;
    let out = '';
    let last = 0;
    let match;
    while ((match = pattern.exec(raw))) {
      out += escapeHighlightHtml(raw.slice(last, match.index));
      const token = match[0];
      let cls = 'md-em';
      if (token[0] === '`') cls = 'md-inline-code';
      else if (token.startsWith('![')) cls = 'md-image';
      else if (token[0] === '[') cls = 'md-link';
      else if (token.startsWith('**') || token.startsWith('__')) cls = 'md-strong';
      else if (token.startsWith('~~')) cls = 'md-strike';
      out += '<span class="' + cls + '">' + escapeHighlightHtml(token) + '</span>';
      last = match.index + token.length;
    }
    return out + escapeHighlightHtml(raw.slice(last));
  }

  function highlightMarkdown(src) {
    const lines = String(src || '').split('\n');
    const out = [];
    let fence = '';
    for (const line of lines) {
      const fenceMatch = line.match(/^\s*(`{3,}|~{3,})/);
      if (fenceMatch) {
        const marker = fenceMatch[1][0];
        if (!fence) {
          fence = marker;
          out.push('<span class="md-code-line md-fence">' + escapeHighlightHtml(line) + '</span>');
          continue;
        }
        if (marker === fence) {
          fence = '';
          out.push('<span class="md-code-line md-fence">' + escapeHighlightHtml(line) + '</span>');
          continue;
        }
      }
      if (fence) {
        out.push('<span class="md-code-line">' + escapeHighlightHtml(line) + '</span>');
        continue;
      }
      if (!line.trim()) {
        out.push('');
        continue;
      }
      let cls = '';
      if (/^\s{0,3}#{1,6}\s+/.test(line)) cls = 'md-heading';
      else if (/^\s{0,3}([-*_])(?:\s*\1){2,}\s*$/.test(line)) cls = 'md-hr';
      else if (/^\s{0,3}>\s?/.test(line)) cls = 'md-quote';
      else if (/^\s{0,3}([-*+]|\d+[.)])\s+/.test(line)) cls = 'md-list';
      const html = highlightMarkdownInline(line);
      out.push(cls ? '<span class="' + cls + '">' + html + '</span>' : html);
    }
    return out.join('\n');
  }

  function detectType(value) {
    if (typeof value === 'boolean') return 'boolean';
    if (typeof value === 'number') return 'number';
    if (value !== null && typeof value === 'object') return 'json';
    return 'string';
  }
  function extraRows(fm) {
    const rows = [];
    Object.keys(fm || {}).forEach((key) => {
      if (KNOWN_FIELDS.indexOf(key) >= 0) return;
      const value = fm[key];
      const type = detectType(value);
      let display = value;
      if (type === 'json') display = JSON.stringify(value, null, 2);
      else if (type === 'boolean') display = value;
      else display = value === undefined || value === null ? '' : String(value);
      rows.push({ key, type, value: display });
    });
    return rows;
  }

  components.ContentEditor = {
    name: 'ContentEditor',
    setup() {
      const route = HP.route;
      const confirm = PrimeVue.useConfirm();
      const isNew = computed(() => route.path === '/content/new');
      const loading = ref(true);
      const saving = ref(false);
      const deleting = ref(false);
      const error = ref('');
      const loadError = ref('');
      const dirty = ref(false);
      const settingUp = ref(false);
      const activeTab = ref(loadEditorTab());
      const mediaPickerVisible = ref(false);
      const coverPickerVisible = ref(false);
      const translating = ref(false);
      const dirDialogVisible = ref(false);
      const newDirName = ref('');
      const creatingDir = ref(false);
      const aiVisible = ref(false);
      const aiTask = ref('polish');
      const aiInstruction = ref('');
      const aiResult = ref('');
      const aiRunning = ref(false);
      const aiError = ref('');
      const aiSelection = ref(null);
      const directories = ref([]);
      const dirValue = ref('');
      const nameValue = ref('');
      const nameTouched = ref(false);
      const urlTouched = ref(false);
      let lastAutoUrl = '';
      const sourceEditor = ref(null);
      const sourceHighlight = ref(null);
      const sourceComposing = ref(false);
      const originalPath = ref('');
      const format = ref('yaml');
      const extras = ref([]);
      const form = reactive({
        title: '',
        path: '',
        slug: '',
        date: HP.toLocalInput(new Date().toISOString()),
        draft: true,
        tags: '',
        categories: '',
        description: '',
        weight: 0,
        url: '',
        author: '',
        type: '',
        image: ''
      });
      const body = ref('');
      const highlightedSource = computed(() => highlightMarkdown(body.value));

      function loadEditorTab() {
        try {
          return localStorage.getItem('hp_editor_tab') === 'source' ? 'source' : 'edit';
        } catch (e) {
          return 'edit';
        }
      }
      watch(activeTab, (value) => {
        if (value !== 'edit' && value !== 'source') return;
        try { localStorage.setItem('hp_editor_tab', value); } catch (e) { /* storage unavailable */ }
      });

      const dirOptions = computed(() => {
        const list = directories.value.slice();
        if (dirValue.value && list.indexOf(dirValue.value) < 0) list.push(dirValue.value);
        list.sort();
        return [{ label: t('content.directory_root', {}, 'content/'), value: '' }]
          .concat(list.map((dir) => ({ label: dir, value: dir })));
      });
      function pathParts(value) {
        const clean = String(value || '').replace(/\\/g, '/').replace(/^\/+/, '');
        const slash = clean.lastIndexOf('/');
        const dir = slash >= 0 ? clean.slice(0, slash) : '';
        const file = slash >= 0 ? clean.slice(slash + 1) : clean;
        return { dir, name: file.replace(/\.(md|markdown|mdown)$/i, '') };
      }
      function setPathFields(value) {
        const parts = pathParts(value);
        dirValue.value = parts.dir;
        nameValue.value = parts.name;
      }
      function onNameInput() {
        nameTouched.value = true;
        const normalized = String(nameValue.value || '').replace(/\s+/g, '-');
        if (normalized !== nameValue.value) nameValue.value = normalized;
      }
      function syncPath() {
        const name = String(nameValue.value || '').trim().replace(/^\/+/, '').replace(/\.(md|markdown|mdown)$/i, '');
        const dir = String(dirValue.value || '').trim().replace(/^\/+|\/+$/g, '');
        form.path = name ? (dir ? dir + '/' + name + '.md' : name + '.md') : '';
      }
      function autoUrl() {
        const name = String(nameValue.value || '').trim().replace(/\.(md|markdown|mdown)$/i, '');
        if (!name) return '';
        const dir = String(dirValue.value || '').trim().replace(/^\/+|\/+$/g, '');
        return '/' + (dir ? dir + '/' : '') + name + '/';
      }
      function refreshAutoUrl() {
        if (urlTouched.value) return;
        const next = autoUrl();
        if (!form.url || form.url === lastAutoUrl) {
          form.url = next;
          lastAutoUrl = next;
        }
      }
      function onUrlInput() {
        urlTouched.value = !!form.url.trim();
        if (!urlTouched.value) refreshAutoUrl();
      }
      watch([dirValue, nameValue], () => { syncPath(); refreshAutoUrl(); });

      function openDirectoryDialog() {
        newDirName.value = '';
        dirDialogVisible.value = true;
      }
      async function createDirectory() {
        const name = String(newDirName.value || '').trim().replace(/^\/+|\/+$/g, '');
        if (!name) {
          HP.toast('error', t('content.directory_name_required', {}, 'Enter a folder name'));
          return;
        }
        creatingDir.value = true;
        try {
          const res = await HP.api('/content/directory', { method: 'POST', body: { path: name } });
          const dir = res && res.path ? res.path : name;
          const meta = await HP.api('/content/meta').catch(() => null);
          if (meta && Array.isArray(meta.directories)) {
            directories.value = meta.directories;
          } else if (directories.value.indexOf(dir) < 0) {
            directories.value = directories.value.concat([dir]);
          }
          dirValue.value = dir;
          dirDialogVisible.value = false;
          HP.toast('success', t('content.directory_created', {}, 'Folder created'), dir);
        } catch (e) {
          HP.notifyError(e);
        } finally {
          creatingDir.value = false;
        }
      }

      const aiTaskOptions = computed(() => [
        { label: t('content.ai_polish', {}, 'Polish writing'), value: 'polish' },
        { label: t('content.ai_continue', {}, 'Continue writing'), value: 'continue' },
        { label: t('content.ai_summary', {}, 'Generate description'), value: 'summary' },
        { label: t('content.ai_custom', {}, 'Custom instruction'), value: 'custom' }
      ]);
      const aiScopeHint = computed(() => aiSelection.value
        ? t('content.ai_selected', {}, 'Applies to the selected text.')
        : t('content.ai_whole', {}, 'Applies to the whole article.'));
      watch(aiTask, () => { aiResult.value = ''; aiError.value = ''; });
      function openAIDialog() {
        aiTask.value = 'polish';
        aiInstruction.value = '';
        aiResult.value = '';
        aiError.value = '';
        aiSelection.value = null;
        if (activeTab.value === 'source') {
          const el = sourceTextarea();
          if (el && typeof el.selectionStart === 'number' && el.selectionEnd > el.selectionStart) {
            aiSelection.value = { start: el.selectionStart, end: el.selectionEnd };
          }
        }
        aiVisible.value = true;
      }
      async function runAI() {
        const task = aiTask.value;
        const instruction = aiInstruction.value.trim();
        if (task === 'custom' && !instruction) {
          aiError.value = t('content.ai_instruction_required', {}, 'Describe what the AI should do');
          return;
        }
        let text = String(body.value || '');
        if ((task === 'polish' || task === 'custom') && aiSelection.value) {
          text = text.slice(aiSelection.value.start, aiSelection.value.end);
        }
        if (!text.trim() && task !== 'continue') {
          aiError.value = t('content.ai_no_text', {}, 'Write some content before using AI');
          return;
        }
        if (task === 'continue' && !text.trim() && !form.title.trim()) {
          aiError.value = t('content.ai_no_text', {}, 'Write some content before using AI');
          return;
        }
        if ((task === 'polish' || task === 'custom') && !aiSelection.value && text.length > 20000) {
          aiError.value = t('content.ai_too_long', {}, 'The article is too long. Select a passage in Markdown view to polish.');
          return;
        }
        if (task === 'continue') text = text.slice(-12000);
        if (task === 'summary') text = text.slice(0, 60000);
        aiRunning.value = true;
        aiError.value = '';
        try {
          const res = await HP.api('/ai/complete', {
            method: 'POST',
            body: { task, instruction, text, title: form.title }
          });
          aiResult.value = (res && res.text) || '';
          if (!aiResult.value) {
            aiError.value = t('content.ai_empty', {}, 'The AI returned an empty result');
          }
        } catch (e) {
          aiError.value = HP.errorText(e);
          HP.notifyError(e);
        } finally {
          aiRunning.value = false;
        }
      }
      function aiApply() {
        const text = String(aiResult.value || '').trim();
        if (!text) return;
        if (aiTask.value === 'summary') {
          form.description = text.replace(/\s+/g, ' ').slice(0, 500);
        } else if (aiTask.value === 'continue') {
          appendSnippet(text);
        } else if (aiSelection.value) {
          const { start, end } = aiSelection.value;
          body.value = body.value.slice(0, start) + text + body.value.slice(end);
          markDirty();
        } else {
          body.value = text;
          markDirty();
        }
        aiVisible.value = false;
        aiResult.value = '';
        HP.toast('success', t('content.ai_applied', {}, 'AI result applied'));
      }

      const pathExt = computed(() => {
        const m = form.path.match(/\.(md|markdown|mdown)$/i);
        return m ? m[1].toLowerCase() : '';
      });
      function countWords(text) {
        const value = text || '';
        const latin = (value.match(/[A-Za-z0-9_'-]+/g) || []).length;
        const cjk = (value.match(/[\u3040-\u30ff\u3400-\u9fff\uac00-\ud7af]/g) || []).length;
        return latin + cjk;
      }
      const wordCount = computed(() => countWords(body.value));
      function escapeHtml(s) {
        return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
      }
      function rewriteMediaLinks(html) {
        return String(html).replace(/(src)=("|')\/images\/([^"']+)\2/g, (m, attr, quote, path) =>
          attr + '=' + quote + '/api/media/file?path=' + encodeURIComponent(path) + quote);
      }
      function renderMarkdown(src) {
        const text = String(src || '');
        if (!text) return '';
        try {
          if (!window.marked || !window.DOMPurify) return '<pre>' + escapeHtml(text) + '</pre>';
          const html = window.marked.parse(text, { gfm: true, breaks: true });
          return rewriteMediaLinks(window.DOMPurify.sanitize(html, { USE_PROFILES: { html: true } }));
        } catch (e) { return '<pre>' + escapeHtml(text) + '</pre>'; }
      }
      const preview = computed(() => {
        if (!body.value) return '<p class="preview-empty">' + t('content.preview_empty', {}, 'Nothing to preview yet.') + '</p>';
        return renderMarkdown(body.value);
      });
      const coverPreview = computed(() => {
        const value = String(form.image || '').trim();
        if (!value) return '';
        if (/^(https?:|data:|blob:)/i.test(value) || value.startsWith('/api/media/')) return value;
        const match = value.match(/^(?:\/|\.\/)?(?:static\/)?images\/(.+)$/);
        if (match) {
          let name = match[1];
          try { name = decodeURIComponent(name); } catch (e) { /* keep raw path */ }
          return '/api/media/file?path=' + encodeURIComponent(name);
        }
        return value;
      });

      // Block-level live editor: rendered blocks, raw markdown only while the block is focused.
      const editingBlock = ref(-1);
      const blockDraft = ref('');
      let editStart = 0;
      let editEnd = 0;
      let editSource = '';
      let blockSeq = 0;

      function splitMarkdownBlocks(src) {
        const text = String(src || '');
        if (!text.trim()) return [];
        let tokens;
        try {
          tokens = window.marked && window.marked.lexer
            ? window.marked.lexer(text)
            : [{ type: 'paragraph', raw: text }];
        } catch (e) {
          tokens = [{ type: 'paragraph', raw: text }];
        }
        const blocks = [];
        let offset = 0;
        for (const token of tokens) {
          const raw = token.raw || '';
          const start = offset;
          offset += raw.length;
          if (token.type === 'space') continue;
          const source = raw.replace(/[\r\n]+$/, '');
          if (!source.trim()) continue;
          const html = renderMarkdown(source);
          blocks.push({
            key: 'block-' + blockSeq++,
            source,
            start,
            end: start + source.length,
            html: html || '<pre class="live-block-raw">' + escapeHtml(source) + '</pre>'
          });
        }
        return blocks;
      }
      const liveBlocks = computed(() => splitMarkdownBlocks(body.value));

      function autoGrowLiveEditor(el) {
        if (!el) return;
        el.style.height = 'auto';
        el.style.height = el.scrollHeight + 'px';
      }
      function focusLiveEditor() {
        nextTick(() => {
          const el = document.querySelector('[data-live-editor]');
          if (!el) return;
          el.focus();
          el.setSelectionRange(el.value.length, el.value.length);
          autoGrowLiveEditor(el);
        });
      }
      function onBlockInput(event) {
        autoGrowLiveEditor(event.target);
        if (!settingUp.value && !loading.value) dirty.value = true;
      }
      function beginBlockEdit(index) {
        if (editingBlock.value === index) return;
        const previous = liveBlocks.value.length ? liveBlocks.value[index] : null;
        const previousStart = previous ? previous.start : null;
        const shiftedFrom = editingBlock.value !== -1 ? editStart : null;
        const delta = editingBlock.value !== -1 ? blockDraft.value.length - editSource.length : 0;
        if (editingBlock.value !== -1) commitBlockEdit();
        let block;
        if (!liveBlocks.value.length) {
          block = { source: body.value, start: 0, end: body.value.length };
        } else if (previousStart !== null && shiftedFrom !== null && previousStart > shiftedFrom) {
          const targetStart = previousStart + delta;
          block = liveBlocks.value.find((b) => b.start === targetStart) || liveBlocks.value[Math.min(index, liveBlocks.value.length - 1)];
        } else {
          block = liveBlocks.value[index];
        }
        if (!block) return;
        const newIndex = liveBlocks.value.indexOf(block);
        editStart = block.start;
        editEnd = block.end;
        editSource = block.source;
        editingBlock.value = newIndex >= 0 ? newIndex : index;
        blockDraft.value = block.source;
        focusLiveEditor();
      }
      function onBlockMouseDown(index, event) {
        if (editingBlock.value === index) return;
        if (event.target.closest('a, button, input, textarea, select')) return;
        event.preventDefault();
        beginBlockEdit(index);
      }
      function commitBlockEdit() {
        const index = editingBlock.value;
        if (index === -1) return;
        editingBlock.value = -1;
        if (blockDraft.value !== editSource) {
          body.value = body.value.slice(0, editStart) + blockDraft.value + body.value.slice(editEnd);
        }
      }
      function onBlockBlur(index) {
        if (editingBlock.value === index) commitBlockEdit();
      }
      const liveWordCount = computed(() => {
        if (editingBlock.value === -1) return wordCount.value;
        return countWords(body.value.slice(0, editStart) + blockDraft.value + body.value.slice(editEnd));
      });
      const metaStatus = computed(() => {
        if (form.draft) return 'draft';
        if (form.date && new Date(HP.fromLocalInput(form.date)) > new Date()) return 'scheduled';
        return 'published';
      });
      const formatOptions = [
        { label: 'YAML', value: 'yaml' },
        { label: 'TOML', value: 'toml' },
        { label: 'JSON', value: 'json' }
      ];
      const typeOptions = computed(() => [
        { label: t('content.type_string', {}, 'Text'), value: 'string' },
        { label: t('content.type_number', {}, 'Number'), value: 'number' },
        { label: t('content.type_boolean', {}, 'Boolean'), value: 'boolean' },
        { label: t('content.type_json', {}, 'JSON'), value: 'json' }
      ]);

      function asciiSlug(value) {
        const out = HP.slugify(value || '');
        return /^[a-z0-9][a-z0-9-]*$/.test(out) ? out : '';
      }
      watch(() => form.title, (title) => {
        if (nameTouched.value) return;
        const suggestion = asciiSlug(title);
        if (suggestion) nameValue.value = suggestion;
      });
      async function translateFilename() {
        const title = form.title.trim();
        if (!title) {
          HP.toast('error', t('content.title_required', {}, 'Add a title before saving'));
          return;
        }
        translating.value = true;
        try {
          const res = await HP.api('/content/slug', { method: 'POST', body: { title } });
          if (res && res.slug) {
            nameTouched.value = true;
            nameValue.value = res.slug;
            if (res.translated) {
              HP.toast('success', t('content.filename_generated', {}, 'English file name generated'));
            } else if (res.reason === 'not_configured') {
              HP.toast('warn', t('content.filename_fallback', {}, 'No translation service configured — used local rules.'), t('content.filename_fallback_hint', {}, 'Configure a provider in Settings → Translation.'));
            } else {
              HP.toast('warn', t('content.filename_failed', {}, 'Translation failed — used local rules.'), t('content.filename_fallback_hint', {}, 'Configure a provider in Settings → Translation.'));
            }
          }
        } catch (e) {
          HP.notifyError(e);
        } finally {
          translating.value = false;
        }
      }

      function markDirty() {
        if (!settingUp.value && !loading.value) dirty.value = true;
      }
      watch(form, markDirty, { deep: true });
      watch(body, markDirty);
      watch(extras, markDirty, { deep: true });

      // The source editor is a transparent textarea drawn over a highlighted
      // <pre>. Keep both scroll positions in sync.
      function syncSourceScroll() {
        const editor = sourceEditor.value;
        const highlight = sourceHighlight.value;
        if (!editor || !highlight) return;
        highlight.scrollTop = editor.scrollTop;
        highlight.scrollLeft = editor.scrollLeft;
      }
      function onSourceInput() {
        markDirty();
        nextTick(syncSourceScroll);
      }
      watch(body, () => { nextTick(syncSourceScroll); });

      function sourceTextarea() {
        if (sourceEditor.value) return sourceEditor.value;
        return findSourceEditor();
      }
      function activeEditorTarget() {
        if (activeTab.value === 'source') {
          const el = sourceTextarea();
          return el ? { kind: 'source', el } : null;
        }
        if (activeTab.value === 'edit' && editingBlock.value !== -1) {
          const el = document.querySelector('textarea[data-live-editor]');
          return el ? { kind: 'live', el } : null;
        }
        return null;
      }
      function targetState(target) {
        const el = target.el;
        const value = target.kind === 'source' ? body.value : blockDraft.value;
        let start = typeof el.selectionStart === 'number' ? el.selectionStart : value.length;
        let end = typeof el.selectionEnd === 'number' ? el.selectionEnd : start;
        if (start > end) { const swap = start; start = end; end = swap; }
        return { value, start: Math.min(start, value.length), end: Math.min(end, value.length) };
      }
      function setTargetValue(target, value, start, end) {
        if (target.kind === 'source') body.value = value;
        else blockDraft.value = value;
        markDirty();
        nextTick(() => {
          const el = target.kind === 'source'
            ? sourceTextarea()
            : document.querySelector('textarea[data-live-editor]');
          if (!el) return;
          el.focus();
          try { el.setSelectionRange(start, end); } catch (e) { /* ignore */ }
          if (target.kind === 'live') autoGrowLiveEditor(el); else syncSourceScroll();
        });
      }
      function appendSnippet(text) {
        const current = body.value;
        const separator = !current ? '' : (/\n\s*$/.test(current) ? '\n' : '\n\n');
        body.value = current + separator + text;
        markDirty();
        activeTab.value = 'source';
        nextTick(() => {
          const el = sourceTextarea();
          if (!el) return;
          const caret = body.value.length;
          el.focus();
          el.setSelectionRange(caret, caret);
          syncSourceScroll();
        });
      }
      function applyEditorChange(transform, fallbackText) {
        const target = activeEditorTarget();
        if (target) {
          const next = transform(targetState(target));
          if (next) setTargetValue(target, next.value, next.start, next.end);
          return;
        }
        if (activeTab.value === 'edit') {
          beginBlockEdit(liveBlocks.value.length ? liveBlocks.value.length - 1 : 0);
          nextTick(() => {
            const later = activeEditorTarget();
            if (later) {
              const next = transform(targetState(later));
              if (next) setTargetValue(later, next.value, next.start, next.end);
            } else if (fallbackText) {
              appendSnippet(fallbackText);
            }
          });
          return;
        }
        if (fallbackText) appendSnippet(fallbackText);
      }
      function wrapSelection(before, after, placeholder) {
        applyEditorChange(({ value, start, end }) => {
          const selected = value.slice(start, end) || placeholder || '';
          const next = value.slice(0, start) + before + selected + after + value.slice(end);
          return {
            value: next,
            start: start + before.length,
            end: start + before.length + selected.length
          };
        }, before + (placeholder || '') + after);
      }
      function toggleLinePrefix(kind, level) {
        applyEditorChange(({ value, start, end }) => {
          const lineStart = value.lastIndexOf('\n', start - 1) + 1;
          let lineEnd = value.indexOf('\n', end);
          if (lineEnd === -1) lineEnd = value.length;
          const lines = value.slice(lineStart, lineEnd).split('\n');
          const filled = lines.filter((line) => line.trim());
          let strip;
          let prefix;
          if (kind === 'heading') {
            strip = /^#{1,6}\s+/;
            prefix = '#'.repeat(level) + ' ';
          } else if (kind === 'quote') {
            strip = /^>\s?/;
            prefix = '> ';
          } else if (kind === 'bullets') {
            strip = /^[-*+]\s+/;
            prefix = '- ';
          } else {
            strip = /^\d+[.)]\s+/;
            prefix = '1. ';
          }
          const allPresent = filled.length > 0 && filled.every((line) => strip.test(line.replace(/^\s*/, '')));
          let ordered = 0;
          const transformed = lines.map((line) => {
            if (!line.trim()) return line;
            const indent = (line.match(/^\s*/) || [''])[0];
            const content = line.slice(indent.length).replace(strip, '');
            const marker = allPresent ? '' : (kind === 'numbers' ? (++ordered) + '. ' : prefix);
            return indent + marker + content;
          }).join('\n');
          return {
            value: value.slice(0, lineStart) + transformed + value.slice(lineEnd),
            start: lineStart,
            end: lineStart + transformed.length
          };
        }, (kind === 'heading' ? '#'.repeat(level) + ' ' : kind === 'quote' ? '> ' : kind === 'bullets' ? '- ' : '1. ') + t('content.fmt_text', {}, 'text'));
      }
      function insertLink() {
        const label = t('content.link_text', {}, 'link text');
        applyEditorChange(({ value, start, end }) => {
          const selected = value.slice(start, end) || label;
          const url = 'https://';
          const next = value.slice(0, start) + '[' + selected + '](' + url + ')' + value.slice(end);
          const urlStart = start + 1 + selected.length + 2;
          return { value: next, start: urlStart, end: urlStart + url.length };
        }, '[' + label + '](https://)');
      }
      function insertCodeBlock() {
        const code = t('content.code_text', {}, 'code');
        applyEditorChange(({ value, start, end }) => {
          const selected = value.slice(start, end) || code;
          const before = start === 0 || value[start - 1] === '\n' ? '' : '\n';
          const after = end >= value.length || value[end] === '\n' ? '' : '\n';
          const next = value.slice(0, start) + before + '```\n' + selected + '\n```' + after + value.slice(end);
          const selectStart = start + before.length + 4;
          return { value: next, start: selectStart, end: selectStart + selected.length };
        }, '```\n' + code + '\n```');
      }
      function insertRule() {
        applyEditorChange(({ value, start, end }) => {
          const before = start > 0 && value[start - 1] !== '\n' ? '\n\n' : '';
          const after = end < value.length && value[end] !== '\n' ? '\n\n' : '\n';
          const next = value.slice(0, start) + before + '---' + after + value.slice(end);
          const caret = start + before.length + 3 + after.length;
          return { value: next, start: caret, end: caret };
        }, '\n---\n');
      }
      const toolbarItems = computed(() => [
        { key: 'bold', label: 'B', cls: 'md-tool-strong', title: t('content.fmt_bold', {}, 'Bold') },
        { key: 'italic', label: 'I', cls: 'md-tool-em', title: t('content.fmt_italic', {}, 'Italic') },
        { key: 'strike', label: 'S', cls: 'md-tool-strike', title: t('content.fmt_strike', {}, 'Strikethrough') },
        { key: 'divider' },
        { key: 'h1', label: 'H1', title: t('content.fmt_h1', {}, 'Heading 1') },
        { key: 'h2', label: 'H2', title: t('content.fmt_h2', {}, 'Heading 2') },
        { key: 'h3', label: 'H3', title: t('content.fmt_h3', {}, 'Heading 3') },
        { key: 'quote', label: '❝', title: t('content.fmt_quote', {}, 'Quote') },
        { key: 'bullets', label: '•', title: t('content.fmt_bullets', {}, 'Bulleted list') },
        { key: 'numbers', label: '1.', title: t('content.fmt_numbers', {}, 'Numbered list') },
        { key: 'divider' },
        { key: 'code', label: '</>', title: t('content.fmt_code', {}, 'Inline code') },
        { key: 'codeblock', label: '{ }', title: t('content.fmt_code_block', {}, 'Code block') },
        { key: 'link', icon: 'pi pi-link', title: t('content.fmt_link', {}, 'Link') },
        { key: 'hr', label: '—', title: t('content.fmt_hr', {}, 'Horizontal rule') },
        { key: 'divider' },
        { key: 'media', icon: 'pi pi-image', cls: 'md-tool-media', title: t('content.fmt_media', {}, 'Insert media') },
        { key: 'ai', icon: 'pi pi-sparkles', cls: 'md-tool-ai', title: t('content.ai_assistant', {}, 'AI assistant') }
      ]);
      function runToolbar(key) {
        if (key === 'bold') wrapSelection('**', '**', t('content.fmt_bold', {}, 'Bold'));
        else if (key === 'italic') wrapSelection('*', '*', t('content.fmt_italic', {}, 'Italic'));
        else if (key === 'strike') wrapSelection('~~', '~~', t('content.fmt_strike', {}, 'Strikethrough'));
        else if (key === 'h1') toggleLinePrefix('heading', 1);
        else if (key === 'h2') toggleLinePrefix('heading', 2);
        else if (key === 'h3') toggleLinePrefix('heading', 3);
        else if (key === 'quote') toggleLinePrefix('quote');
        else if (key === 'bullets') toggleLinePrefix('bullets');
        else if (key === 'numbers') toggleLinePrefix('numbers');
        else if (key === 'code') wrapSelection('`', '`', t('content.code_text', {}, 'code'));
        else if (key === 'codeblock') insertCodeBlock();
        else if (key === 'link') insertLink();
        else if (key === 'hr') insertRule();
        else if (key === 'media') openMediaPicker();
        else if (key === 'ai') openAIDialog();
      }
      function onEditorKeydown(event) {
        if (!(event.metaKey || event.ctrlKey) || event.altKey) return;
        const key = String(event.key || '').toLowerCase();
        if (key === 'b') { event.preventDefault(); runToolbar('bold'); }
        else if (key === 'i') { event.preventDefault(); runToolbar('italic'); }
        else if (key === 'k') { event.preventDefault(); runToolbar('link'); }
      }

      function frontMatterString(item, key) {
        const fm = item && item.front_matter;
        const value = fm ? fm[key] : '';
        return value === undefined || value === null ? '' : String(value);
      }
      function setFormFromItem(item) {
        settingUp.value = true;
        originalPath.value = item.path;
        form.title = item.title || '';
        form.path = item.path;
        form.slug = item.slug || '';
        form.date = HP.toLocalInput(item.date);
        form.draft = !!item.draft;
        form.tags = (item.tags || []).join(', ');
        form.categories = (item.categories || []).join(', ');
        form.description = item.description || '';
        form.weight = item.weight || 0;
        form.url = frontMatterString(item, 'url');
        form.author = frontMatterString(item, 'author');
        form.type = frontMatterString(item, 'type');
        form.image = frontMatterString(item, 'image');
        body.value = (item.body || '').replace(/\r\n?/g, '\n');
        format.value = item.format || 'yaml';
        extras.value = extraRows(item.front_matter || {});
        setPathFields(item.path);
        nameTouched.value = true;
        urlTouched.value = false;
        lastAutoUrl = '';
        refreshAutoUrl();
        nextTick(() => { dirty.value = false; settingUp.value = false; });
      }
      async function load() {
        loading.value = true;
        loadError.value = '';
        try {
          const meta = await HP.api('/content/meta').catch(() => null);
          if (meta && Array.isArray(meta.directories)) directories.value = meta.directories;
          if (isNew.value) {
            settingUp.value = true;
            dirValue.value = String(route.query.section || '').trim().replace(/^\/+|\/+$/g, '');
            nameValue.value = '';
            nameTouched.value = false;
            urlTouched.value = false;
            lastAutoUrl = '';
            syncPath();
            form.date = HP.toLocalInput(new Date().toISOString());
            nextTick(() => { dirty.value = false; settingUp.value = false; });
          } else {
            const path = route.query.path;
            if (!path) throw new HP.ApiError('invalid_request', t('content.path_required', {}, 'Content path is required'));
            const item = await HP.api('/content/item?path=' + encodeURIComponent(path));
            setFormFromItem(item);
          }
        } catch (e) {
          loadError.value = HP.errorText(e);
        } finally {
          loading.value = false;
        }
      }

      function splitList(v) {
        return String(v || '').split(',').map((s) => s.trim()).filter(Boolean);
      }
      function buildFrontMatter() {
        const fm = {};
        for (const row of extras.value) {
          const key = String(row.key || '').trim();
          if (!key) continue;
          if (KNOWN_FIELDS.indexOf(key) >= 0) {
            throw new HP.ApiError('invalid_request', t('content.reserved_field', { key }, '“{key}” is edited above and cannot be added again.'));
          }
          let value = row.value;
          if (row.type === 'number') {
            value = Number(String(value).trim());
            if (!isFinite(value)) throw new HP.ApiError('invalid_request', t('content.invalid_number', { key }, '“{key}” must be a number.'));
          } else if (row.type === 'boolean') {
            value = !!value;
          } else if (row.type === 'json') {
            try { value = JSON.parse(value); }
            catch (e) { throw new HP.ApiError('invalid_request', t('content.invalid_json', { key }, '“{key}” contains invalid JSON.')); }
          } else {
            value = String(value === undefined || value === null ? '' : value);
          }
          fm[key] = value;
        }
        if (form.title.trim()) fm.title = form.title.trim();
        if (form.slug.trim()) fm.slug = form.slug.trim(); else delete fm.slug;
        if (form.date) fm.date = HP.fromLocalInput(form.date); else delete fm.date;
        fm.draft = !!form.draft;
        const tags = splitList(form.tags);
        if (tags.length) fm.tags = tags; else delete fm.tags;
        const cats = splitList(form.categories);
        if (cats.length) fm.categories = cats; else delete fm.categories;
        if (form.description.trim()) fm.description = form.description.trim(); else delete fm.description;
        if (Number(form.weight)) fm.weight = Number(form.weight); else delete fm.weight;
        if (form.url.trim()) fm.url = form.url.trim(); else delete fm.url;
        if (form.author.trim()) fm.author = form.author.trim(); else delete fm.author;
        if (form.type.trim()) fm.type = form.type.trim(); else delete fm.type;
        if (form.image.trim()) fm.image = form.image.trim(); else delete fm.image;
        return fm;
      }
      async function save() {
        error.value = '';
        commitBlockEdit();
        if (!form.title.trim()) { error.value = t('content.title_required', {}, 'Add a title before saving'); return; }
        syncPath();
        if (!nameValue.value.trim()) { error.value = t('content.filename_required', {}, 'Choose a file name before saving'); return; }
        if (!form.path.trim() || !pathExt.value) { error.value = t('content.path_invalid', {}, 'Path must end in .md, .markdown or .mdown'); return; }
        saving.value = true;
        try {
          const fm = buildFrontMatter();
          const payload = {
            path: isNew.value ? form.path : originalPath.value,
            new_path: isNew.value ? '' : form.path,
            format: format.value,
            front_matter: fm,
            body: body.value
          };
          const item = isNew.value
            ? await HP.api('/content', { method: 'POST', body: payload })
            : await HP.api('/content', { method: 'PUT', body: payload });
          dirty.value = false;
          HP.toast('success', isNew.value ? t('content.created', {}, 'Content created') : t('content.updated', {}, 'Content saved'));
          if (isNew.value || item.path !== originalPath.value) {
            HP.navigate('/content/edit', { path: item.path });
          } else {
            setFormFromItem(item);
          }
        } catch (e) {
          error.value = HP.errorText(e);
          HP.notifyError(e);
        } finally {
          saving.value = false;
        }
      }
      function confirmLeave(then) {
        if (!dirty.value) { then(); return; }
        confirm.require({
          message: t('content.unsaved_confirm', {}, 'Discard your unsaved changes?'),
          header: t('content.unsaved', {}, 'Unsaved changes'),
          icon: 'pi pi-exclamation-triangle',
          acceptClass: 'p-button-danger',
          acceptLabel: t('content.discard', {}, 'Discard'),
          rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: then
        });
      }
      function back() { confirmLeave(() => HP.navigate('/content')); }
      function remove() {
        confirm.require({
          message: t('content.delete_confirm', { title: form.title }, 'Delete “{title}”? This cannot be undone.'),
          header: t('content.delete', {}, 'Delete content'),
          icon: 'pi pi-trash', acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'),
          rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            deleting.value = true;
            try {
              await HP.api('/content?path=' + encodeURIComponent(originalPath.value), { method: 'DELETE' });
              dirty.value = false;
              HP.toast('success', t('content.deleted', {}, 'Content deleted'));
              HP.navigate('/content');
            } catch (e) { HP.notifyError(e); }
            finally { deleting.value = false; }
          }
        });
      }
      function addExtra() { extras.value.push({ key: '', type: 'string', value: '' }); }
      function removeExtra(index) { extras.value.splice(index, 1); }

      // Insert media links at the caret of whichever editor is active.
      let pendingInsert = null;
      function findSourceEditor() {
        const direct = document.querySelector('textarea.markdown-editor');
        if (direct) return direct;
        const wrapper = document.querySelector('.markdown-editor');
        if (!wrapper) return null;
        return wrapper.tagName === 'TEXTAREA' ? wrapper : wrapper.querySelector('textarea');
      }
      function openMediaPicker() {
        pendingInsert = null;
        if (activeTab.value === 'source') {
          const el = findSourceEditor();
          if (el) pendingInsert = { type: 'source', start: el.selectionStart, end: el.selectionEnd, value: body.value };
        } else if (editingBlock.value !== -1) {
          const el = document.querySelector('textarea[data-live-editor]');
          if (el) {
            pendingInsert = {
              type: 'live', blockStart: editStart, start: el.selectionStart, end: el.selectionEnd,
              value: blockDraft.value
            };
          }
        }
        mediaPickerVisible.value = true;
      }
      function openCoverPicker() { coverPickerVisible.value = true; }
      function chooseCover(entry) {
        if (!entry) return;
        form.image = entry.url || '';
        coverPickerVisible.value = false;
      }
      function clearCover() { form.image = ''; }
      function insertMedia(payload) {
        const text = payload && payload.text;
        if (!text) return;
        // The picker steals focus, so use the selection captured when it opened.
        if (pendingInsert && pendingInsert.type === 'source') {
          const start = pendingInsert.start === null ? body.value.length : pendingInsert.start;
          const end = pendingInsert.end === null ? start : pendingInsert.end;
          body.value = body.value.slice(0, start) + text + body.value.slice(end);
          const caret = start + text.length;
          nextTick(() => {
            const el = findSourceEditor();
            if (el) { el.focus(); el.setSelectionRange(caret, caret); }
          });
          markDirty();
          pendingInsert = null;
          return;
        }
        if (pendingInsert && pendingInsert.type === 'live') {
          if (editingBlock.value !== -1 && editStart === pendingInsert.blockStart) {
            const start = pendingInsert.start === null ? blockDraft.value.length : pendingInsert.start;
            const end = pendingInsert.end === null ? start : pendingInsert.end;
            blockDraft.value = blockDraft.value.slice(0, start) + text + blockDraft.value.slice(end);
            const caret = start + text.length;
            nextTick(() => {
              const el = document.querySelector('textarea[data-live-editor]');
              if (el) { el.focus(); el.setSelectionRange(caret, caret); }
            });
            markDirty();
            pendingInsert = null;
            return;
          }
          // The block was committed when the picker opened; insert into the body.
          const start = pendingInsert.blockStart + (pendingInsert.start || 0);
          const end = pendingInsert.blockStart + (pendingInsert.end === null ? (pendingInsert.start || 0) : pendingInsert.end);
          body.value = body.value.slice(0, start) + text + body.value.slice(end);
          markDirty();
          pendingInsert = null;
          return;
        }
        if (activeTab.value === 'source') {
          const el = findSourceEditor();
          if (el) {
            const start = el.selectionStart === null ? body.value.length : el.selectionStart;
            const end = el.selectionEnd === null ? start : el.selectionEnd;
            body.value = body.value.slice(0, start) + text + body.value.slice(end);
            const caret = start + text.length;
            nextTick(() => { el.focus(); el.setSelectionRange(caret, caret); });
            markDirty();
            return;
          }
        }
        if (editingBlock.value !== -1) {
          const el = document.querySelector('textarea[data-live-editor]');
          const start = el && el.selectionStart !== null ? el.selectionStart : blockDraft.value.length;
          const end = el && el.selectionEnd !== null ? el.selectionEnd : start;
          blockDraft.value = blockDraft.value.slice(0, start) + text + blockDraft.value.slice(end);
          const caret = start + text.length;
          nextTick(() => { if (el) { el.focus(); el.setSelectionRange(caret, caret); } });
          markDirty();
          return;
        }
        const prefix = !body.value ? '' : (/\n\s*$/.test(body.value) ? '\n' : '\n\n');
        body.value += prefix + text + '\n';
        markDirty();
      }
      watch(mediaPickerVisible, (visible) => { if (!visible) pendingInsert = null; });
      function beforeUnload(e) {
        if (!dirty.value) return;
        e.preventDefault();
        e.returnValue = '';
      }
      window.addEventListener('beforeunload', beforeUnload);
      onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload));
      onMounted(load);

      return {
        isNew, loading, saving, deleting, error, loadError, dirty, activeTab, form, body, format, extras,
        formatOptions, typeOptions, wordCount: liveWordCount, preview, metaStatus, pathExt, load, save, back,
        liveBlocks, editingBlock, blockDraft, beginBlockEdit, onBlockMouseDown, commitBlockEdit, onBlockBlur, onBlockInput,
        remove, addExtra, removeExtra, confirmLeave, mediaPickerVisible, openMediaPicker, insertMedia, go: HP.navigate,
        coverPickerVisible, coverPreview, openCoverPicker, chooseCover, clearCover,
        directories, dirValue, nameValue, dirOptions, translating, translateFilename, onUrlInput, onNameInput,
        dirDialogVisible, newDirName, creatingDir, openDirectoryDialog, createDirectory,
        aiVisible, aiTask, aiInstruction, aiResult, aiRunning, aiError, aiTaskOptions, aiScopeHint,
        openAIDialog, runAI, aiApply,
        sourceEditor, sourceHighlight, highlightedSource, syncSourceScroll, onSourceInput, onEditorKeydown,
        sourceComposing,
        toolbarItems, runToolbar,
        formatDate: HP.formatDate, t
      };
    },
    template: `
      <div class="page editor-page">
        <PageHeader :eyebrow="t('content.library', {}, 'Library')" :title="isNew ? t('content.new', {}, 'New content') : t('content.edit', {}, 'Edit content')" :subtitle="form.path || t('content.editor_subtitle', {}, 'Draft, preview and publish Markdown.')" live>
          <template #actions>
            <PButton text :label="t('common.back', {}, 'Back')" icon="pi pi-arrow-left" @click="back" />
            <PButton v-if="!isNew" text severity="danger" icon="pi pi-trash" :loading="deleting" @click="remove" :title="t('common.delete')" />
            <PButton :label="t('content.save', {}, 'Save content')" icon="pi pi-check" :loading="saving" @click="save" class="btn-primary" />
          </template>
        </PageHeader>

        <div v-if="loading" class="editor-loading"><PProgressSpinner strokeWidth="3" /><span>{{ t('common.loading', {}, 'Loading…') }}</span></div>
        <PMessage v-else-if="loadError" severity="error" :closable="false">{{ loadError }} <button class="link-button" @click="go('/content')">{{ t('common.back', {}, 'Back') }}</button></PMessage>

        <template v-else>
          <div v-if="dirty" class="unsaved-banner"><i class="pi pi-circle-fill"></i>{{ t('content.unsaved', {}, 'Unsaved changes') }}</div>
          <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>

          <div class="editor-grid">
            <div class="editor-main">
              <PCard class="panel-card">
                <template #content>
                  <label class="field">
                    <span>{{ t('content.title_field', {}, 'Title') }}</span>
                    <PInputText v-model="form.title" class="text-xl-input" :placeholder="t('content.title_placeholder', {}, 'A clear, memorable title')" />
                  </label>
                </template>
              </PCard>

              <PCard class="panel-card editor-body-card">
                <template #content>
                  <div v-if="activeTab !== 'preview'" class="markdown-toolbar" role="toolbar" :aria-label="t('content.formatting', {}, 'Formatting')">
                    <template v-for="item in toolbarItems" :key="item.key">
                      <span v-if="item.key === 'divider'" class="md-tool-sep" aria-hidden="true"></span>
                      <button v-else type="button" class="md-tool" :class="item.cls" :title="item.title" :aria-label="item.title" @mousedown.prevent @click="runToolbar(item.key)">
                        <i v-if="item.icon" :class="item.icon"></i>
                        <span v-else>{{ item.label }}</span>
                      </button>
                    </template>
                  </div>
                  <PTabs v-model:value="activeTab">
                    <PTabList>
                      <PTab value="edit"><i class="pi pi-pencil"></i> {{ t('content.editor', {}, 'Editor') }}</PTab>
                      <PTab value="source"><i class="pi pi-code"></i> Markdown</PTab>
                      <PTab value="preview"><i class="pi pi-eye"></i> {{ t('content.preview', {}, 'Preview') }}</PTab>
                    </PTabList>
                    <PTabPanels>
                      <PTabPanel value="edit">
                        <div class="markdown-live">
                          <div v-if="!liveBlocks.length && editingBlock === -1" class="live-block live-placeholder" @mousedown.prevent="beginBlockEdit(0)">
                            {{ t('content.body_placeholder', {}, 'Write in Markdown. Front matter is handled for you.') }}
                          </div>
                          <div v-for="(block, index) in liveBlocks" :key="block.key" class="live-block" :class="{ 'is-editing': editingBlock === index }" @mousedown="onBlockMouseDown(index, $event)">
                            <textarea v-if="editingBlock === index" data-live-editor class="live-block-editor" v-model="blockDraft" rows="1" spellcheck="false" @input="onBlockInput" @blur="onBlockBlur(index)" @keydown="onEditorKeydown" @keydown.esc.prevent="commitBlockEdit"></textarea>
                            <div v-else class="markdown-body live-block-rendered" v-html="block.html"></div>
                          </div>
                          <div v-if="!liveBlocks.length && editingBlock === 0" class="live-block is-editing">
                            <textarea data-live-editor class="live-block-editor" v-model="blockDraft" rows="1" spellcheck="false" @input="onBlockInput" @blur="onBlockBlur(0)" @keydown="onEditorKeydown" @keydown.esc.prevent="commitBlockEdit"></textarea>
                          </div>
                        </div>
                      </PTabPanel>
                      <PTabPanel value="source">
                        <div class="md-highlight-wrap" :class="{ 'is-composing': sourceComposing }">
                          <pre ref="sourceHighlight" class="md-highlight" aria-hidden="true"><code v-html="highlightedSource"></code></pre>
                          <textarea ref="sourceEditor" v-model="body" class="markdown-editor" spellcheck="false" :placeholder="t('content.body_placeholder', {}, 'Write in Markdown. Front matter is handled for you.')" @scroll="syncSourceScroll" @input="onSourceInput" @keydown="onEditorKeydown" @compositionstart="sourceComposing = true" @compositionend="sourceComposing = false"></textarea>
                        </div>
                      </PTabPanel>
                      <PTabPanel value="preview">
                        <div class="markdown-body preview-pane" v-html="preview"></div>
                      </PTabPanel>
                    </PTabPanels>
                  </PTabs>
                  <div class="editor-footer"><span>{{ t('content.words', {}, 'Words') }}: {{ wordCount }}</span><span>{{ t('content.live_edit_hint', {}, 'Click a block to edit its Markdown; click elsewhere to see it rendered.') }}</span></div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head">
                    <div><h2>{{ t('content.extra_fields', {}, 'Additional front matter') }}</h2><p>{{ t('content.extra_fields_hint', {}, 'Keep theme-specific fields intact or add new ones.') }}</p></div>
                    <PButton text icon="pi pi-plus" :label="t('content.add_field', {}, 'Add field')" @click="addExtra" />
                  </div>
                  <div v-if="extras.length === 0" class="empty-inline compact"><i class="pi pi-sliders-h"></i><span>{{ t('content.no_extra_fields', {}, 'No extra front matter fields.') }}</span></div>
                  <div v-for="(row, index) in extras" :key="index" class="extra-row">
                    <PInputText v-model="row.key" :placeholder="t('content.field_key', {}, 'key')" class="mono-input" />
                    <PSelect v-model="row.type" :options="typeOptions" optionLabel="label" optionValue="value" class="extra-type" />
                    <template v-if="row.type === 'boolean'">
                      <div class="extra-bool"><PToggleSwitch v-model="row.value" /><span>{{ row.value ? 'true' : 'false' }}</span></div>
                    </template>
                    <PTextarea v-else-if="row.type === 'json'" v-model="row.value" rows="2" autoResize class="mono-input" :placeholder="'{ }'" />
                    <PInputText v-else v-model="row.value" :placeholder="t('content.field_value', {}, 'value')" />
                    <PButton text rounded severity="danger" icon="pi pi-times" @click="removeExtra(index)" :title="t('common.remove')" />
                  </div>
                </template>
              </PCard>
            </div>

            <aside class="editor-side">
              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('content.publishing', {}, 'Publishing') }}</h2><p>{{ t('content.publishing_hint', {}, 'Status and format') }}</p></div><StatusTag :status="metaStatus" /></div>
                  <div class="switch-row">
                    <div><strong>{{ t('content.draft', {}, 'Draft') }}</strong><small>{{ t('content.draft_hint', {}, 'Hide this page from the live site') }}</small></div>
                    <PToggleSwitch v-model="form.draft" />
                  </div>
                  <div class="form-stack">
                    <label class="field">
                      <span>{{ t('content.date', {}, 'Publish date') }}</span>
                      <PInputText type="datetime-local" v-model="form.date" />
                    </label>
                    <label class="field">
                      <span>{{ t('content.author', {}, 'Author') }}</span>
                      <PInputText v-model="form.author" :placeholder="t('content.author_hint', {}, 'Defaults to the site author')" />
                    </label>
                    <label class="field">
                      <span>{{ t('content.type_field', {}, 'Type') }}</span>
                      <PInputText v-model="form.type" :placeholder="t('content.type_hint', {}, 'posts')" />
                    </label>
                    <div class="field-grid">
                      <label class="field">
                        <span>{{ t('content.weight', {}, 'Weight') }}</span>
                        <PInputNumber v-model="form.weight" :min="0" :max="100000" showButtons class="w-full" />
                      </label>
                      <label class="field">
                        <span>{{ t('content.front_matter_format', {}, 'Front matter format') }}</span>
                        <PSelect v-model="format" :options="formatOptions" optionLabel="label" optionValue="value" class="w-full" />
                      </label>
                    </div>
                  </div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('content.link_settings', {}, 'File and URL') }}</h2><p>{{ t('content.link_settings_hint', {}, 'Where this page lives and how it is linked.') }}</p></div></div>
                  <div class="form-stack">
                    <div class="field">
                      <span>{{ t('content.directory', {}, 'Directory') }}</span>
                      <div class="dir-row">
                        <PSelect v-model="dirValue" :options="dirOptions" optionLabel="label" optionValue="value" filter class="w-full" />
                        <PButton icon="pi pi-folder-plus" :title="t('content.directory_new', {}, 'New folder')" :aria-label="t('content.directory_new', {}, 'New folder')" @click="openDirectoryDialog" />
                      </div>
                      <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('content.directory_hint', {}, 'Subdirectory inside content/.') }}</small>
                    </div>
                    <div class="field">
                      <span>{{ t('content.filename', {}, 'File name') }}</span>
                      <div class="filename-row">
                        <PInputText v-model="nameValue" @input="onNameInput" class="mono-input" :placeholder="t('content.filename_hint', {}, 'my-first-post')" />
                        <PButton icon="pi pi-language" :loading="translating" :label="t('content.translate_filename', {}, 'English')" :title="t('content.translate_filename_hint', {}, 'Translate the title into an English file name')" @click="translateFilename" />
                      </div>
                      <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('content.filename_note', {}, 'Spaces become dashes. The .md extension is added automatically.') }}</small>
                    </div>
                    <label class="field">
                      <span>{{ t('content.url', {}, 'URL') }} <em>{{ t('content.optional', {}, 'optional') }}</em></span>
                      <PInputText v-model="form.url" @input="onUrlInput" class="mono-input" :placeholder="t('content.url_hint', {}, '/about/ or https://example.com/')" />
                      <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('content.url_field_hint', {}, 'Custom permalink written to the “url” front matter field.') }}</small>
                    </label>
                    <label class="field">
                      <span>{{ t('content.slug', {}, 'Slug') }} <em>{{ t('content.optional', {}, 'optional') }}</em></span>
                      <PInputText v-model="form.slug" class="mono-input" :placeholder="t('content.slug_hint', {}, 'my-first-post')" />
                    </label>
                  </div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('content.summary_cover', {}, 'Summary and cover') }}</h2><p>{{ t('content.summary_cover_hint', {}, 'Search previews and social cards.') }}</p></div></div>
                  <div class="form-stack">
                    <label class="field">
                      <span>{{ t('content.description', {}, 'Description') }}</span>
                      <PTextarea v-model="form.description" rows="2" autoResize :placeholder="t('content.description_hint', {}, 'Shown in search results and social cards')" />
                    </label>
                    <div class="field">
                      <span>{{ t('content.cover_image', {}, 'Cover image') }} <em>{{ t('content.optional', {}, 'optional') }}</em></span>
                      <div class="cover-input-row">
                        <PInputText v-model="form.image" class="mono-input" :placeholder="t('content.cover_placeholder', {}, '/images/cover.jpg')" />
                        <PButton icon="pi pi-images" :label="t('content.choose_cover', {}, 'Media library')" @click="openCoverPicker" />
                        <PButton v-if="form.image" text severity="danger" icon="pi pi-times" :title="t('content.remove_cover', {}, 'Remove cover image')" @click="clearCover" />
                      </div>
                      <div v-if="form.image" class="cover-preview">
                        <img :key="coverPreview" :src="coverPreview" alt="" @error="$event.target.style.visibility = 'hidden'" />
                      </div>
                      <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('content.cover_hint', {}, 'Written to the “image” front matter field. Pick a file from the media library or paste a URL.') }}</small>
                    </div>
                    <label class="field">
                      <span>{{ t('content.tags', {}, 'Tags') }}</span>
                      <PInputText v-model="form.tags" :placeholder="t('content.tags_hint', {}, 'design, Hugo, writing')" />
                    </label>
                    <label class="field">
                      <span>{{ t('content.categories', {}, 'Categories') }}</span>
                      <PInputText v-model="form.categories" :placeholder="t('content.categories_hint', {}, 'journal')" />
                    </label>
                  </div>
                </template>
              </PCard>

              <PCard class="panel-card">
                <template #content>
                  <div class="side-meta">
                    <div><span>{{ t('content.words', {}, 'Words') }}</span><strong>{{ wordCount }}</strong></div>
                    <div><span>{{ t('content.path', {}, 'Path') }}</span><strong class="break-all">{{ form.path || '—' }}</strong></div>
                  </div>
                  <div class="action-stack">
                    <PButton class="w-full btn-primary" :label="t('content.save', {}, 'Save content')" icon="pi pi-check" :loading="saving" @click="save" />
                    <PButton class="w-full" text :label="t('content.unsaved_back', {}, 'Back to content')" @click="back" />
                  </div>
                </template>
              </PCard>
            </aside>
          </div>
        </template>

        <MediaPicker v-model:visible="mediaPickerVisible" @insert="insertMedia" />
        <MediaPicker v-model:visible="coverPickerVisible" mode="choose" images-only @select="chooseCover" />

        <PDialog v-model:visible="dirDialogVisible" modal :header="t('content.directory_new', {}, 'New folder')" :style="{ width: '26rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <label class="field">
              <span>{{ t('content.directory_name', {}, 'Folder name') }}</span>
              <PInputText v-model="newDirName" class="mono-input" placeholder="posts/2026" @keydown.enter.prevent="createDirectory" />
              <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('content.directory_name_hint', {}, 'Use / for nested folders, e.g. posts/2026.') }}</small>
            </label>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="dirDialogVisible = false" />
            <PButton :label="t('content.directory_create', {}, 'Create folder')" icon="pi pi-folder-plus" :loading="creatingDir" @click="createDirectory" class="btn-primary" />
          </template>
        </PDialog>

        <PDialog v-model:visible="aiVisible" modal :header="t('content.ai_assistant', {}, 'AI assistant')" :style="{ width: '42rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <PMessage severity="info" :closable="false">{{ t('content.ai_privacy', {}, 'The content is sent to the AI provider configured in Settings.') }}</PMessage>
            <label class="field">
              <span>{{ t('content.ai_task', {}, 'Action') }}</span>
              <PSelect v-model="aiTask" :options="aiTaskOptions" optionLabel="label" optionValue="value" class="w-full" />
            </label>
            <label class="field">
              <span>{{ t('content.ai_instruction', {}, 'Instruction') }} <em v-if="aiTask !== 'custom'">{{ t('content.optional', {}, 'optional') }}</em></span>
              <PTextarea v-model="aiInstruction" rows="2" autoResize :placeholder="t('content.ai_instruction_hint', {}, 'e.g. make it more concise and keep the code blocks')" />
            </label>
            <small v-if="aiTask === 'polish' || aiTask === 'custom'" class="field-hint"><i class="pi pi-info-circle"></i>{{ aiScopeHint }}</small>
            <PMessage v-if="aiError" severity="error" :closable="false">{{ aiError }}</PMessage>
            <label v-if="aiResult" class="field">
              <span>{{ t('content.ai_result', {}, 'Result') }}</span>
              <PTextarea v-model="aiResult" rows="10" class="mono-input" />
            </label>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="aiVisible = false" />
            <template v-if="aiResult">
              <PButton :label="t('content.ai_regenerate', {}, 'Generate again')" icon="pi pi-refresh" :loading="aiRunning" @click="runAI" />
              <PButton :label="t('content.ai_apply', {}, 'Apply')" icon="pi pi-check" class="btn-primary" @click="aiApply" />
            </template>
            <PButton v-else :label="t('content.ai_generate', {}, 'Generate')" icon="pi pi-sparkles" class="btn-primary" :loading="aiRunning" @click="runAI" />
          </template>
        </PDialog>
      </div>
    `
  };
})();
