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

  const KNOWN_FIELDS = ['title', 'slug', 'date', 'draft', 'description', 'tags', 'categories', 'weight'];

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
      const activeTab = ref('edit');
      const mediaPickerVisible = ref(false);
      const originalPath = ref('');
      const format = ref('yaml');
      const extras = ref([]);
      const form = reactive({
        title: '',
        path: '',
        folder: route.query.section || '',
        slug: '',
        date: HP.toLocalInput(new Date().toISOString()),
        draft: true,
        tags: '',
        categories: '',
        description: '',
        weight: 0
      });
      const body = ref('');
      const pathTouched = ref(false);

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

      function suggestedPath() {
        if (isNew.value && !pathTouched.value) {
          const folder = form.folder.trim().replace(/^\/+|\/+$/g, '');
          const name = HP.slugify(form.slug || form.title || 'untitled');
          form.path = (folder ? folder + '/' : '') + name + '.md';
        }
      }
      watch(() => [form.title, form.slug, form.folder], () => { if (isNew.value && !pathTouched.value) suggestedPath(); });

      function markDirty() {
        if (!settingUp.value && !loading.value) dirty.value = true;
      }
      watch(form, markDirty, { deep: true });
      watch(body, markDirty);
      watch(extras, markDirty, { deep: true });

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
        body.value = (item.body || '').replace(/\r\n?/g, '\n');
        format.value = item.format || 'yaml';
        extras.value = extraRows(item.front_matter || {});
        pathTouched.value = true;
        nextTick(() => { dirty.value = false; settingUp.value = false; });
      }
      async function load() {
        loading.value = true;
        loadError.value = '';
        try {
          if (isNew.value) {
            settingUp.value = true;
            form.path = '';
            form.folder = route.query.section || form.folder || '';
            suggestedPath();
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
        return fm;
      }
      async function save() {
        error.value = '';
        commitBlockEdit();
        if (!form.title.trim()) { error.value = t('content.title_required', {}, 'Add a title before saving'); return; }
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
      function onPathInput() { pathTouched.value = true; }

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
        remove, addExtra, removeExtra, onPathInput, confirmLeave, mediaPickerVisible, openMediaPicker, insertMedia, go: HP.navigate,
        formatDate: HP.formatDate, t
      };
    },
    template: `
      <div class="page editor-page">
        <PageHeader :eyebrow="t('content.library', {}, 'Library')" :title="isNew ? t('content.new', {}, 'New content') : t('content.edit', {}, 'Edit content')" :subtitle="form.path || t('content.editor_subtitle', {}, 'Draft, preview and publish Markdown.')" live>
          <template #actions>
            <PButton text :label="t('common.back', {}, 'Back')" icon="pi pi-arrow-left" @click="back" />
            <PButton text icon="pi pi-image" :label="t('media.insert_media', {}, 'Insert media')" @click="openMediaPicker" />
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
                  <div class="form-stack">
                    <label class="field">
                      <span>{{ t('content.title_field', {}, 'Title') }}</span>
                      <PInputText v-model="form.title" class="text-xl-input" :placeholder="t('content.title_placeholder', {}, 'A clear, memorable title')" />
                    </label>
                    <div class="field-grid" v-if="isNew">
                      <label class="field">
                        <span>{{ t('content.folder', {}, 'Folder / section') }}</span>
                        <PInputText v-model="form.folder" :placeholder="t('content.folder_hint', {}, 'posts')" />
                      </label>
                      <label class="field">
                        <span>{{ t('content.slug', {}, 'Slug') }}</span>
                        <PInputText v-model="form.slug" :placeholder="t('content.slug_hint', {}, 'my-first-post')" />
                      </label>
                    </div>
                    <label class="field">
                      <span>{{ t('content.path', {}, 'File path') }}</span>
                      <PInputText v-model="form.path" @input="onPathInput" class="mono-input" />
                      <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('content.path_hint', {}, 'Relative to content/. Changing this renames the file.') }}</small>
                    </label>
                    <div class="field-grid">
                      <label class="field">
                        <span>{{ t('content.date', {}, 'Publish date') }}</span>
                        <PInputText type="datetime-local" v-model="form.date" />
                      </label>
                      <label class="field">
                        <span>{{ t('content.weight', {}, 'Weight') }}</span>
                        <PInputNumber v-model="form.weight" :min="0" :max="100000" showButtons class="w-full" />
                      </label>
                    </div>
                    <label class="field">
                      <span>{{ t('content.description', {}, 'Description') }}</span>
                      <PTextarea v-model="form.description" rows="2" autoResize :placeholder="t('content.description_hint', {}, 'Shown in search results and social cards')" />
                    </label>
                    <div class="field-grid">
                      <label class="field">
                        <span>{{ t('content.tags', {}, 'Tags') }}</span>
                        <PInputText v-model="form.tags" :placeholder="t('content.tags_hint', {}, 'design, hugo, writing')" />
                      </label>
                      <label class="field">
                        <span>{{ t('content.categories', {}, 'Categories') }}</span>
                        <PInputText v-model="form.categories" :placeholder="t('content.categories_hint', {}, 'journal')" />
                      </label>
                    </div>
                  </div>
                </template>
              </PCard>

              <PCard class="panel-card editor-body-card">
                <template #content>
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
                            <textarea v-if="editingBlock === index" data-live-editor class="live-block-editor" v-model="blockDraft" rows="1" spellcheck="false" @input="onBlockInput" @blur="onBlockBlur(index)" @keydown.esc.prevent="commitBlockEdit"></textarea>
                            <div v-else class="markdown-body live-block-rendered" v-html="block.html"></div>
                          </div>
                          <div v-if="!liveBlocks.length && editingBlock === 0" class="live-block is-editing">
                            <textarea data-live-editor class="live-block-editor" v-model="blockDraft" rows="1" spellcheck="false" @input="onBlockInput" @blur="onBlockBlur(0)" @keydown.esc.prevent="commitBlockEdit"></textarea>
                          </div>
                        </div>
                      </PTabPanel>
                      <PTabPanel value="source">
                        <PTextarea v-model="body" class="markdown-editor" autoResize :placeholder="t('content.body_placeholder', {}, 'Write in Markdown. Front matter is handled for you.')" />
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
              <PCard class="panel-card sticky-card">
                <template #content>
                  <div class="panel-head"><div><h2>{{ t('content.publishing', {}, 'Publishing') }}</h2><p>{{ t('content.publishing_hint', {}, 'Status and format') }}</p></div><StatusTag :status="metaStatus" /></div>
                  <div class="switch-row">
                    <div><strong>{{ t('content.draft', {}, 'Draft') }}</strong><small>{{ t('content.draft_hint', {}, 'Hide this page from the live site') }}</small></div>
                    <PToggleSwitch v-model="form.draft" />
                  </div>
                  <label class="field">
                    <span>{{ t('content.front_matter_format', {}, 'Front matter format') }}</span>
                    <PSelect v-model="format" :options="formatOptions" optionLabel="label" optionValue="value" class="w-full" />
                  </label>
                  <div class="side-meta">
                    <div><span>{{ t('content.slug', {}, 'Slug') }}</span><strong>{{ form.slug || '—' }}</strong></div>
                    <div><span>{{ t('content.words', {}, 'Words') }}</span><strong>{{ wordCount }}</strong></div>
                    <div><span>{{ t('content.path', {}, 'Path') }}</span><strong class="break-all">{{ form.path }}</strong></div>
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
      </div>
    `
  };
})();
