(function () {
  'use strict';
  const HP = window.HP;
  const { ref, reactive, computed, watch, onMounted } = Vue;
  const t = HP.t;
  const components = HP.components = HP.components || {};

  const LIBRARY_KEY = 'hp_media_library';

  function queryString(params) {
    const q = new URLSearchParams();
    Object.keys(params || {}).forEach((key) => {
      const value = params[key];
      if (value !== undefined && value !== null && value !== '') q.set(key, value);
    });
    return q.toString();
  }

  function kindIcon(kind) {
    switch (kind) {
      case 'image': return 'pi pi-image';
      case 'video': return 'pi pi-video';
      case 'audio': return 'pi pi-volume-up';
      case 'pdf': return 'pi pi-file-pdf';
      case 'text': return 'pi pi-file-edit';
      default: return 'pi pi-file';
    }
  }

  function libraryTypeLabel(type) {
    if (type === 's3') return t('media.type_s3', {}, 'S3');
    if (type === 'webdav') return t('media.type_webdav', {}, 'WebDAV');
    return t('media.type_local', {}, 'Local folder');
  }

  function kindLabel(kind) {
    switch (kind) {
      case 'image': return t('media.kind_image', {}, 'Image');
      case 'video': return t('media.kind_video', {}, 'Video');
      case 'audio': return t('media.kind_audio', {}, 'Audio');
      case 'pdf': return t('media.kind_pdf', {}, 'PDF');
      case 'text': return t('media.kind_text', {}, 'Text');
      default: return t('media.kind_file', {}, 'File');
    }
  }

  function markdownFor(entry) {
    if (!entry) return '';
    if (entry.kind === 'image') {
      const alt = String(entry.name || '').replace(/\.[^.]+$/, '');
      return '![' + alt + '](' + entry.url + ')';
    }
    if (entry.kind === 'video') {
      return '<video controls src="' + entry.url + '"></video>';
    }
    return '[' + entry.name + '](' + entry.url + ')';
  }

  function acceptAttribute(extensions) {
    return (extensions || []).join(',');
  }

  function humanError(e) { return HP.errorText(e); }

  /* ------------------------------------------------------------------ */
  /* Media page                                                          */
  /* ------------------------------------------------------------------ */

  components.MediaPage = {
    name: 'MediaPage',
    setup() {
      const confirm = PrimeVue.useConfirm();
      const libraries = ref([]);
      const allowedExtensions = ref([]);
      const defaultMaxUpload = ref(512);
      const libraryId = ref(localStorage.getItem(LIBRARY_KEY) || 'local');
      const current = ref(null);
      const path = ref('');
      const entries = ref([]);
      const stats = ref({ files: 0, bytes: 0 });
      const loading = ref(true);
      const uploading = ref(false);
      const progress = ref('');
      const error = ref('');
      const dragging = ref(false);
      const fileInput = ref(null);
      const maxUploadMB = ref(512);

      const previewVisible = ref(false);
      const selected = ref(null);
      const previewText = ref('');
      const previewLoading = ref(false);

      const folderVisible = ref(false);
      const folderName = ref('');
      const folderBusy = ref(false);

      const transferVisible = ref(false);
      const transfer = reactive({ mode: 'move', entry: null, library: '', folder: '', name: '' });
      const transferBusy = ref(false);

      const storageVisible = ref(false);
      const libraryFormVisible = ref(false);
      const libraryFormEditing = ref(false);
      const libraryTesting = ref(false);
      const librarySaving = ref(false);
      const libraryTestError = ref('');
      const blankLibrary = () => ({
        id: '', name: '', type: 'local', root: '', public_base: '',
        endpoint: '', region: 'us-east-1', bucket: '', access_key: '', secret_key: '',
        base_url: '', username: '', password: '', prefix: '', path_style: false,
        insecure_skip_verify: false, host_ip: '', doh_url: '', max_upload_mb: 512
      });
      const libraryForm = reactive(blankLibrary());
      const libraryHasSecret = ref(false);

      const typeOptions = computed(() => [
        { label: t('media.type_local', {}, 'Local folder'), value: 'local' },
        { label: t('media.type_s3', {}, 'S3'), value: 's3' },
        { label: t('media.type_webdav', {}, 'WebDAV'), value: 'webdav' }
      ]);

      const currentLibrary = computed(() => current.value || libraries.value.find((l) => l.id === libraryId.value) || null);
      const libraryHostname = computed(() => {
        const raw = libraryForm.type === 's3' ? libraryForm.endpoint : libraryForm.base_url;
        try { return new URL(raw).hostname; } catch (e) { return ''; }
      });
      const crumbs = computed(() => {
        const out = [{ label: t('media.root', {}, 'Root'), path: '' }];
        let acc = '';
        String(path.value || '').split('/').filter(Boolean).forEach((segment) => {
          acc = acc ? acc + '/' + segment : segment;
          out.push({ label: segment, path: acc });
        });
        return out;
      });
      const folderEntries = computed(() => entries.value.filter((e) => e.is_dir));
      const fileEntries = computed(() => entries.value.filter((e) => !e.is_dir));
      const accept = computed(() => acceptAttribute(allowedExtensions.value));

      async function loadLibraries() {
        try {
          const data = await HP.api('/media/libraries');
          libraries.value = (data.libraries || []).map((library) => library.builtin ? Object.assign({}, library, { name: t('media.builtin_library', {}, 'Site images') }) : library);
          allowedExtensions.value = data.allowed_extensions || [];
          defaultMaxUpload.value = data.default_max_upload_mb || 512;
          if (!libraries.value.some((l) => l.id === libraryId.value)) {
            libraryId.value = data.default || 'local';
          }
        } catch (e) { error.value = humanError(e); }
      }

      async function load() {
        loading.value = true;
        error.value = '';
        try {
          const data = await HP.api('/media?' + queryString({ library: libraryId.value, path: path.value }));
          current.value = data.library || null;
          if (data.library && data.library.id) libraryId.value = data.library.id;
          path.value = data.path || '';
          entries.value = data.entries || [];
          stats.value = data.stats || { files: 0, bytes: 0 };
          maxUploadMB.value = data.max_upload_mb || defaultMaxUpload.value;
          try { localStorage.setItem(LIBRARY_KEY, libraryId.value); } catch (e) {}
        } catch (e) {
          error.value = humanError(e);
          entries.value = [];
        } finally {
          loading.value = false;
        }
      }

      async function refresh() {
        await loadLibraries();
        await load();
      }

      function selectLibrary(id) {
        if (id === libraryId.value) return;
        libraryId.value = id;
        path.value = '';
        load();
      }
      function openFolder(entry) {
        path.value = entry.path;
        load();
      }
      function goCrumb(crumb) {
        path.value = crumb.path;
        load();
      }
      function chooseFiles() { if (fileInput.value) fileInput.value.click(); }
      function onInput(event) { uploadFiles(event.target.files); }
      function onDrop(event) {
        dragging.value = false;
        uploadFiles(event.dataTransfer.files);
      }

      async function uploadFiles(fileList) {
        const list = Array.from(fileList || []);
        if (!list.length) return;
        uploading.value = true;
        error.value = '';
        let ok = 0;
        for (let i = 0; i < list.length; i++) {
          const file = list[i];
          if (file.size > maxUploadMB.value * 1024 * 1024) {
            HP.toast('error', t('media.too_large', {}, 'File is too large'), file.name);
            continue;
          }
          progress.value = t('media.uploading_progress', { current: i + 1, total: list.length }, 'Uploading {current} of {total}…');
          const fd = new FormData();
          fd.append('file', file);
          try {
            await HP.api('/media?' + queryString({ library: libraryId.value, path: path.value }), { method: 'POST', body: fd });
            ok++;
          } catch (e) {
            HP.notifyError(e);
          }
        }
        uploading.value = false;
        progress.value = '';
        if (fileInput.value) fileInput.value.value = '';
        if (ok) {
          HP.toast('success', t('media.uploaded', { count: ok }, '{count} file(s) uploaded'));
          load();
        }
      }

      function createFolder() {
        const name = folderName.value.trim();
        if (!name) { HP.toast('error', t('media.folder_required', {}, 'Enter a folder name')); return; }
        folderBusy.value = true;
        const target = path.value ? path.value + '/' + name : name;
        HP.api('/media/folder', { method: 'POST', body: { library: libraryId.value, path: target } })
          .then(() => {
            folderVisible.value = false;
            folderName.value = '';
            HP.toast('success', t('media.folder_created', {}, 'Folder created'));
            load();
          })
          .catch((e) => HP.notifyError(e))
          .finally(() => { folderBusy.value = false; });
      }

      async function openPreview(entry) {
        selected.value = entry;
        previewVisible.value = true;
        previewText.value = '';
        if (entry.kind === 'text') {
          previewLoading.value = true;
          try {
            const res = await fetch(entry.preview_url, { credentials: 'same-origin' });
            if (res.ok) previewText.value = (await res.text()).slice(0, 200000);
          } catch (e) { /* keep the download link */ }
          finally { previewLoading.value = false; }
        }
      }
      function copyUrl(entry) { HP.copyText(entry.url); }
      function copyMarkdown(entry) { HP.copyText(markdownFor(entry)); }

      function openTransfer(mode, entry) {
        transfer.mode = mode;
        transfer.entry = entry;
        transfer.library = libraryId.value;
        transfer.folder = path.value;
        transfer.name = entry.name;
        transferVisible.value = true;
      }
      function submitTransfer() {
        const name = transfer.name.trim();
        if (!name) { HP.toast('error', t('media.path_required', {}, 'Enter a file name')); return; }
        const folder = transfer.folder.trim().replace(/^\/+|\/+$/g, '');
        const to = folder ? folder + '/' + name : name;
        const from = transfer.entry.path;
        if (to === from && transfer.library === libraryId.value) return;
        transferBusy.value = true;
        HP.api('/media/' + transfer.mode, {
          method: 'POST',
          body: { from_library: libraryId.value, from, to_library: transfer.library || libraryId.value, to }
        }).then(() => {
          transferVisible.value = false;
          HP.toast('success', transfer.mode === 'move' ? t('media.moved', {}, 'File moved') : t('media.copied', {}, 'File copied'));
          load();
        }).catch((e) => HP.notifyError(e))
          .finally(() => { transferBusy.value = false; });
      }

      function remove(entry) {
        confirm.require({
          message: t('media.delete_confirm', { name: entry.name }, 'Delete “{name}”? Files used by content will stop loading.'),
          header: t('media.delete', {}, 'Delete file'),
          icon: 'pi pi-trash', acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'), rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            try {
              await HP.api('/media?' + queryString({ library: libraryId.value, path: entry.path }), { method: 'DELETE' });
              entries.value = entries.value.filter((x) => x.path !== entry.path);
              stats.value.files = Math.max(0, stats.value.files - 1);
              stats.value.bytes = Math.max(0, stats.value.bytes - entry.size);
              HP.toast('success', t('media.deleted', {}, 'File deleted'));
            } catch (e) { HP.notifyError(e); }
          }
        });
      }

      /* ----- storage management ----- */
      function openStorage() {
        storageVisible.value = true;
        libraryFormVisible.value = false;
        loadLibraries();
      }
      function resetLibraryForm() {
        Object.assign(libraryForm, blankLibrary());
        libraryForm.max_upload_mb = defaultMaxUpload.value;
        libraryHasSecret.value = false;
        libraryFormEditing.value = false;
        libraryTestError.value = '';
      }
      function addLibrary() {
        resetLibraryForm();
        libraryFormVisible.value = true;
      }
      function editLibrary(library) {
        resetLibraryForm();
        Object.assign(libraryForm, {
          id: library.id, name: library.name, type: library.type, root: library.root || '',
          public_base: library.public_base || '', endpoint: library.endpoint || '',
          region: library.region || 'us-east-1', bucket: library.bucket || '',
          access_key: library.access_key || '', secret_key: '', base_url: library.base_url || '',
          username: library.username || '', password: '', prefix: library.prefix || '',
          path_style: !!library.path_style, insecure_skip_verify: !!library.insecure_skip_verify,
          host_ip: library.host_ip || '', doh_url: library.doh_url || '',
          max_upload_mb: library.max_upload_mb || defaultMaxUpload.value
        });
        libraryHasSecret.value = !!library.has_secret;
        libraryFormEditing.value = true;
        libraryFormVisible.value = true;
      }
      function testLibrary() {
        libraryTesting.value = true;
        libraryTestError.value = '';
        const payload = Object.assign({}, libraryForm, { max_upload_mb: Number(libraryForm.max_upload_mb) || 512 });
        HP.api('/media/libraries/test', { method: 'POST', body: payload })
          .then(() => HP.toast('success', t('media.test_ok', {}, 'Connection works')))
          .catch((e) => {
            libraryTestError.value = humanError(e);
            HP.notifyError(e);
          })
          .finally(() => { libraryTesting.value = false; });
      }
      function saveLibrary() {
        librarySaving.value = true;
        const payload = Object.assign({}, libraryForm, { max_upload_mb: Number(libraryForm.max_upload_mb) || 512 });
        HP.api('/media/libraries', { method: 'POST', body: payload })
          .then((res) => {
            const saved = res.library;
            libraryFormVisible.value = false;
            HP.toast('success', t('media.library_saved', {}, 'Storage saved'));
            loadLibraries().then(() => {
              if (saved && saved.id) {
                libraryId.value = saved.id;
                path.value = '';
                load();
              }
            });
          })
          .catch((e) => HP.notifyError(e))
          .finally(() => { librarySaving.value = false; });
      }
      function deleteLibrary(library) {
        confirm.require({
          message: t('media.delete_library_confirm', { name: library.name }, 'Delete the storage “{name}”? Files are not removed from the server.'),
          header: t('media.delete_library', {}, 'Delete storage'),
          icon: 'pi pi-trash', acceptClass: 'p-button-danger',
          acceptLabel: t('common.delete', {}, 'Delete'), rejectLabel: t('common.cancel', {}, 'Cancel'),
          accept: async () => {
            try {
              await HP.api('/media/libraries?id=' + encodeURIComponent(library.id), { method: 'DELETE' });
              HP.toast('success', t('media.library_deleted', {}, 'Storage deleted'));
              if (libraryId.value === library.id) { libraryId.value = 'local'; path.value = ''; }
              await loadLibraries();
              await load();
            } catch (e) { HP.notifyError(e); }
          }
        });
      }

      onMounted(async () => {
        await loadLibraries();
        await load();
      });

      return {
        libraries, allowedExtensions, libraryId, current, path, entries, stats, loading, uploading, progress,
        error, dragging, fileInput, maxUploadMB, previewVisible, selected, previewText, previewLoading,
        folderVisible, folderName, folderBusy, transferVisible, transfer, transferBusy,
        storageVisible, libraryFormVisible, libraryFormEditing, libraryForm, libraryHasSecret,
        libraryTesting, librarySaving, libraryTestError, typeOptions, currentLibrary, libraryHostname, crumbs, folderEntries, fileEntries, accept,
        refresh, selectLibrary, openFolder, goCrumb, chooseFiles, onInput, onDrop,
        createFolder, openPreview, copyUrl, copyMarkdown, openTransfer, submitTransfer, remove,
        openStorage, addLibrary, editLibrary, testLibrary, saveLibrary, deleteLibrary,
        kindIcon, kindLabel, libraryTypeLabel, markdownFor,
        formatBytes: HP.formatBytes, formatDate: HP.formatDate, t
      };
    },
    template: `
      <div class="page">
        <PageHeader :eyebrow="t('media.library', {}, 'Assets')" :title="t('media.title', {}, 'Media')" :subtitle="t('media.subtitle', {}, 'Upload and reuse files for your content.')">
          <template #actions>
            <PButton text rounded icon="pi pi-refresh" @click="refresh" :title="t('common.refresh')" />
            <PButton text :label="t('media.new_folder', {}, 'New folder')" icon="pi pi-folder-plus" @click="folderVisible = true" />
            <PButton :label="t('media.upload', {}, 'Upload files')" icon="pi pi-upload" :loading="uploading" @click="chooseFiles" class="btn-primary" />
            <input ref="fileInput" type="file" :accept="accept" multiple class="hidden-input" @change="onInput" />
          </template>
        </PageHeader>

        <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>

        <div class="media-toolbar">
          <div class="media-library-picker">
            <label class="field compact-field">
              <span>{{ t('media.storage', {}, 'Storage') }}</span>
              <PSelect :modelValue="libraryId" :options="libraries" optionLabel="name" optionValue="id" @update:modelValue="selectLibrary" class="media-library-select" />
            </label>
            <PTag v-if="currentLibrary" :value="libraryTypeLabel(currentLibrary.type)" severity="secondary" />
            <PTag v-if="currentLibrary && currentLibrary.builtin" :value="t('media.builtin', {}, 'Built-in')" severity="info" />
          </div>
          <div class="media-toolbar-right">
            <span class="media-stats">{{ formatBytes(stats.bytes) }} · {{ stats.files }} {{ t('media.files', {}, 'files') }}</span>
            <PButton text icon="pi pi-server" :label="t('media.manage_storage', {}, 'Storage settings')" @click="openStorage" />
          </div>
        </div>

        <div class="media-breadcrumb" aria-label="Breadcrumb">
          <template v-for="(crumb, index) in crumbs" :key="crumb.path">
            <button class="crumb" :class="{ current: index === crumbs.length - 1 }" @click="goCrumb(crumb)">{{ crumb.label }}</button>
            <i v-if="index < crumbs.length - 1" class="pi pi-angle-right"></i>
          </template>
        </div>

        <div class="media-dropzone" :class="{ dragging }" role="button" tabindex="0" @click="chooseFiles" @keydown.enter="chooseFiles" @keydown.space.prevent="chooseFiles" @dragover.prevent="dragging = true" @dragleave.prevent="dragging = false" @drop.prevent="onDrop">
          <span class="drop-icon"><i class="pi pi-cloud-upload"></i></span>
          <div><strong>{{ uploading ? (progress || t('media.uploading', {}, 'Uploading…')) : t('media.drop_title', {}, 'Drop files here or click to upload') }}</strong>
            <span>{{ t('media.drop_hint', { size: formatBytes(maxUploadMB * 1024 * 1024) }, 'Images, video, audio, PDF and documents · up to {size}') }}</span></div>
          <span class="media-stats">{{ formatBytes(stats.bytes) }} · {{ stats.files }} {{ t('media.files', {}, 'files') }}</span>
        </div>

        <div v-if="loading" class="media-grid">
          <PCard v-for="n in 8" :key="n" class="media-card"><template #content><PSkeleton height="160px" /></template></PCard>
        </div>
        <EmptyState v-else-if="entries.length === 0" icon="pi pi-images" :title="t('media.empty_title', {}, 'This folder is empty')" :text="t('media.empty_text', {}, 'Upload a file or create a folder to organise your media.')">
          <PButton :label="t('media.upload', {}, 'Upload files')" icon="pi pi-upload" @click="chooseFiles" />
        </EmptyState>
        <template v-else>
          <div v-if="folderEntries.length" class="media-folders">
            <button v-for="folder in folderEntries" :key="folder.path" class="media-folder" @click="openFolder(folder)">
              <i class="pi pi-folder"></i>
              <span>{{ folder.name }}</span>
              <i class="pi pi-angle-right"></i>
            </button>
          </div>
          <div class="media-grid">
            <PCard v-for="f in fileEntries" :key="f.path" class="media-card">
              <template #content>
                <button class="media-thumb" @click="openPreview(f)" :title="t('media.preview', {}, 'Preview')">
                  <img v-if="f.kind === 'image'" :src="f.preview_url" :alt="f.name" loading="lazy" />
                  <span v-else class="media-kind-icon">
                    <i :class="kindIcon(f.kind)"></i>
                    <em>{{ kindLabel(f.kind) }}</em>
                  </span>
                </button>
                <div class="media-info">
                  <div class="media-name" :title="f.path">{{ f.name }}</div>
                  <div class="media-meta"><span v-if="f.width">{{ f.width }}×{{ f.height }}</span><span>{{ formatBytes(f.size) }}</span><span>{{ formatDate(f.mod_time) }}</span></div>
                </div>
                <div class="media-actions">
                  <PButton text rounded icon="pi pi-eye" @click.stop="openPreview(f)" :title="t('media.preview', {}, 'Preview')" />
                  <PButton text rounded icon="pi pi-link" @click.stop="copyUrl(f)" :title="t('media.copy_url', {}, 'Copy URL')" />
                  <PButton text rounded icon="pi pi-code" @click.stop="copyMarkdown(f)" :title="t('media.copy_markdown', {}, 'Copy Markdown')" />
                  <PButton text rounded icon="pi pi-arrow-right" @click.stop="openTransfer('move', f)" :title="t('media.move', {}, 'Move')" />
                  <PButton text rounded icon="pi pi-copy" @click.stop="openTransfer('copy', f)" :title="t('media.copy', {}, 'Copy')" />
                  <PButton text rounded severity="danger" icon="pi pi-trash" @click.stop="remove(f)" :title="t('common.delete')" />
                </div>
              </template>
            </PCard>
          </div>
        </template>

        <PDialog v-model:visible="previewVisible" modal :header="selected ? selected.name : ''" :style="{ width: '52rem', maxWidth: '96vw' }" class="media-preview-dialog">
          <div v-if="selected" class="media-preview">
            <div v-if="selected.kind === 'image'" class="preview-media"><img :src="selected.preview_url" :alt="selected.name" /></div>
            <div v-else-if="selected.kind === 'video'" class="preview-media"><video :src="selected.preview_url" controls playsinline preload="metadata"></video></div>
            <div v-else-if="selected.kind === 'audio'" class="preview-audio"><audio :src="selected.preview_url" controls preload="metadata"></audio></div>
            <div v-else-if="selected.kind === 'pdf'" class="preview-frame"><iframe :src="selected.preview_url" :title="selected.name"></iframe></div>
            <div v-else-if="selected.kind === 'text'" class="preview-text">
              <div v-if="previewLoading" class="editor-loading"><PProgressSpinner strokeWidth="3" /><span>{{ t('common.loading', {}, 'Loading…') }}</span></div>
              <pre v-else>{{ previewText || t('media.preview_empty', {}, 'No preview available.') }}</pre>
            </div>
            <div v-else class="preview-file">
              <i :class="kindIcon(selected.kind)"></i>
              <p>{{ selected.name }}</p>
              <a :href="selected.preview_url" :download="selected.name" class="btn-link">{{ t('media.download', {}, 'Download file') }}</a>
            </div>
            <dl class="media-detail-list">
              <div><dt>{{ t('media.type', {}, 'Type') }}</dt><dd>{{ kindLabel(selected.kind) }} · {{ selected.content_type }}</dd></div>
              <div><dt>{{ t('media.file_size', {}, 'Size') }}</dt><dd>{{ formatBytes(selected.size) }}</dd></div>
              <div><dt>{{ t('media.folder', {}, 'Folder') }}</dt><dd class="break-all">{{ selected.path }}</dd></div>
              <div><dt>{{ t('media.public_url', {}, 'URL') }}</dt><dd class="break-all mono-inline">{{ selected.url }}</dd></div>
            </dl>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Close')" @click="previewVisible = false" />
            <PButton text icon="pi pi-link" :label="t('media.copy_url', {}, 'Copy URL')" @click="copyUrl(selected)" />
            <PButton text icon="pi pi-code" :label="t('media.copy_markdown', {}, 'Copy Markdown')" @click="copyMarkdown(selected)" />
            <a :href="selected ? selected.preview_url : '#'" target="_blank" rel="noopener" class="p-button p-button-text" v-if="selected">
              <i class="pi pi-external-link"></i><span>{{ t('media.open', {}, 'Open') }}</span>
            </a>
          </template>
        </PDialog>

        <PDialog v-model:visible="folderVisible" modal :header="t('media.new_folder', {}, 'New folder')" :style="{ width: '26rem', maxWidth: '94vw' }">
          <label class="field"><span>{{ t('media.folder_name', {}, 'Folder name') }}</span><PInputText v-model="folderName" :placeholder="t('media.folder_hint', {}, '2026/photos')" @keydown.enter="createFolder" /></label>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="folderVisible = false" />
            <PButton :label="t('media.create_folder', {}, 'Create folder')" icon="pi pi-folder-plus" :loading="folderBusy" @click="createFolder" />
          </template>
        </PDialog>

        <PDialog v-model:visible="transferVisible" modal :header="transfer.mode === 'move' ? t('media.move_file', {}, 'Move file') : t('media.copy_file', {}, 'Copy file')" :style="{ width: '32rem', maxWidth: '94vw' }">
          <div class="form-stack">
            <PMessage severity="info" :closable="false">{{ transfer.entry ? transfer.entry.path : '' }}</PMessage>
            <label class="field"><span>{{ t('media.target_storage', {}, 'Storage') }}</span>
              <PSelect v-model="transfer.library" :options="libraries" optionLabel="name" optionValue="id" class="w-full" />
            </label>
            <label class="field"><span>{{ t('media.destination_folder', {}, 'Destination folder') }}</span><PInputText v-model="transfer.folder" :placeholder="t('media.root', {}, 'Root')" /></label>
            <label class="field"><span>{{ t('media.file_name', {}, 'File name') }}</span><PInputText v-model="transfer.name" class="mono-input" /></label>
          </div>
          <template #footer>
            <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="transferVisible = false" />
            <PButton :label="transfer.mode === 'move' ? t('media.move', {}, 'Move') : t('media.copy', {}, 'Copy')" :loading="transferBusy" @click="submitTransfer" />
          </template>
        </PDialog>

        <PDialog v-model:visible="storageVisible" modal :header="t('media.manage_storage', {}, 'Storage settings')" :style="{ width: '46rem', maxWidth: '96vw' }">
          <template v-if="!libraryFormVisible">
            <div class="storage-list">
              <div v-for="library in libraries" :key="library.id" class="storage-row">
                <span class="storage-icon"><i :class="library.type === 'local' ? 'pi pi-folder' : (library.type === 's3' ? 'pi pi-cloud' : 'pi pi-server')"></i></span>
                <div class="storage-info">
                  <strong>{{ library.name }}</strong>
                  <small>{{ libraryTypeLabel(library.type) }}<template v-if="library.bucket"> · {{ library.bucket }}</template><template v-if="library.prefix"> / {{ library.prefix }}</template></small>
                  <small v-if="library.public_base" class="break-all">{{ library.public_base }}</small>
                </div>
                <PTag v-if="library.builtin" :value="t('media.builtin', {}, 'Built-in')" severity="info" />
                <template v-else>
                  <PButton text rounded icon="pi pi-pencil" @click="editLibrary(library)" :title="t('common.edit')" />
                  <PButton text rounded severity="danger" icon="pi pi-trash" @click="deleteLibrary(library)" :title="t('common.delete')" />
                </template>
              </div>
            </div>
            <PButton class="w-full" outlined icon="pi pi-plus" :label="t('media.add_storage', {}, 'Add storage')" @click="addLibrary" />
          </template>
          <template v-else>
            <div class="form-stack">
              <PMessage v-if="libraryTestError" severity="error" :closable="false">{{ libraryTestError }}</PMessage>
              <div class="field-grid">
                <label class="field"><span>{{ t('media.name', {}, 'Name') }}</span><PInputText v-model="libraryForm.name" :placeholder="t('media.name_hint', {}, 'Media CDN')" /></label>
                <label class="field"><span>{{ t('media.type', {}, 'Type') }}</span><PSelect v-model="libraryForm.type" :options="typeOptions" optionLabel="label" optionValue="value" class="w-full" /></label>
              </div>
              <label class="field"><span>{{ t('media.public_base', {}, 'Public base URL') }}</span><PInputText v-model="libraryForm.public_base" placeholder="https://cdn.example.com/media" class="mono-input" />
                <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('media.public_base_hint', {}, 'Files are addressed as base URL + folder path. Leave blank to use the storage endpoint.') }}</small>
              </label>
              <template v-if="libraryForm.type === 'local'">
                <label class="field"><span>{{ t('media.local_root', {}, 'Server directory') }}</span><PInputText v-model="libraryForm.root" placeholder="/srv/media" class="mono-input" />
                  <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('media.local_root_hint', {}, 'Absolute path on the server. The folder is created when needed.') }}</small>
                </label>
              </template>
              <template v-else-if="libraryForm.type === 's3'">
                <div class="field-grid">
                  <label class="field"><span>{{ t('media.endpoint', {}, 'Endpoint') }}</span><PInputText v-model="libraryForm.endpoint" placeholder="https://s3.amazonaws.com" class="mono-input" /></label>
                  <label class="field"><span>{{ t('media.region', {}, 'Region') }}</span><PInputText v-model="libraryForm.region" placeholder="us-east-1" class="mono-input" /></label>
                </div>
                <div class="field-grid">
                  <label class="field"><span>{{ t('media.bucket', {}, 'Bucket') }}</span><PInputText v-model="libraryForm.bucket" placeholder="media" class="mono-input" /></label>
                  <label class="field"><span>{{ t('media.prefix', {}, 'Prefix / folder') }}</span><PInputText v-model="libraryForm.prefix" placeholder="assets" class="mono-input" /></label>
                </div>
                <div class="field-grid">
                  <label class="field"><span>{{ t('media.access_key', {}, 'Access key') }}</span><PInputText v-model="libraryForm.access_key" class="mono-input" autocomplete="off" /></label>
                  <label class="field"><span>{{ t('media.secret_key', {}, 'Secret key') }}</span><PPassword v-model="libraryForm.secret_key" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" :placeholder="libraryHasSecret ? t('media.secret_keep', {}, 'Saved — leave blank to keep') : ''" /></label>
                </div>
                <div class="switch-row">
                  <div><strong>{{ t('media.path_style', {}, 'Path-style endpoint') }}</strong><small>{{ t('media.path_style_hint', {}, 'Enable for MinIO and most S3-compatible services.') }}</small></div>
                  <PToggleSwitch v-model="libraryForm.path_style" />
                </div>
              </template>
              <template v-else-if="libraryForm.type === 'webdav'">
                <label class="field"><span>{{ t('media.webdav_url', {}, 'WebDAV URL') }}</span><PInputText v-model="libraryForm.base_url" placeholder="https://cloud.example.com/remote.php/dav/files/user" class="mono-input" /></label>
                <div class="field-grid">
                  <label class="field"><span>{{ t('media.username', {}, 'Username') }}</span><PInputText v-model="libraryForm.username" autocomplete="off" /></label>
                  <label class="field"><span>{{ t('media.password', {}, 'Password') }}</span><PPassword v-model="libraryForm.password" :feedback="false" toggleMask inputClass="w-full" autocomplete="new-password" :placeholder="libraryHasSecret ? t('media.secret_keep', {}, 'Saved — leave blank to keep') : ''" /></label>
                </div>
                <label class="field"><span>{{ t('media.prefix', {}, 'Prefix / folder') }}</span><PInputText v-model="libraryForm.prefix" placeholder="media" class="mono-input" /></label>
              </template>
              <label v-if="libraryForm.type !== 'local'" class="field">
                <span>{{ t('media.host_ip', {}, 'Host IP (optional)') }}</span>
                <PInputText v-model="libraryForm.host_ip" placeholder="192.168.2.10" class="mono-input" />
                <small class="field-hint"><i class="pi pi-info-circle"></i>
                  {{ libraryHostname
                    ? t('media.host_ip_hint', { host: libraryHostname }, 'Dial this IP instead of resolving {host}. TLS and the Host header still use the hostname. Useful when Docker DNS cannot resolve the NAS.')
                    : t('media.host_ip_hint_empty', {}, 'Optional: dial this IP instead of resolving the URL hostname. TLS and the Host header still use the hostname.') }}
                </small>
              </label>
              <label v-if="libraryForm.type !== 'local'" class="field">
                <span>{{ t('media.doh_url', {}, 'DNS over HTTPS (optional)') }}</span>
                <PInputText v-model="libraryForm.doh_url" placeholder="https://1.1.1.1/dns-query" class="mono-input" />
                <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('media.doh_url_hint', {}, 'Resolve the storage hostname through this DoH endpoint over HTTPS when the container cannot reach normal DNS. Use an IP-based endpoint such as https://1.1.1.1/dns-query. Ignored when Host IP is set.') }}</small>
              </label>
              <div v-if="libraryForm.type !== 'local'" class="switch-row">
                <div><strong>{{ t('media.insecure_tls', {}, 'Allow self-signed certificates') }}</strong><small>{{ t('media.insecure_tls_hint', {}, 'Only enable this for a server you trust. It disables TLS certificate verification for this storage.') }}</small></div>
                <PToggleSwitch v-model="libraryForm.insecure_skip_verify" />
              </div>
              <label class="field"><span>{{ t('media.max_upload', {}, 'Maximum upload size (MB)') }}</span><PInputNumber v-model="libraryForm.max_upload_mb" :min="1" :max="4096" :showButtons="false" class="w-full" />
                <small class="field-hint"><i class="pi pi-info-circle"></i>{{ t('media.max_upload_hint', {}, 'Your reverse proxy and storage provider may impose a lower limit.') }}</small>
              </label>
            </div>
          </template>
          <template #footer>
            <PButton v-if="libraryFormVisible" text :label="t('media.back_to_list', {}, 'Back')" @click="libraryFormVisible = false" />
            <PButton text :label="t('common.cancel', {}, 'Close')" @click="storageVisible = false" />
            <PButton v-if="libraryFormVisible" text icon="pi pi-bolt" :label="t('media.test_connection', {}, 'Test connection')" :loading="libraryTesting" @click="testLibrary" />
            <PButton v-if="libraryFormVisible" icon="pi pi-check" :label="t('common.save_changes', {}, 'Save')" :loading="librarySaving" @click="saveLibrary" />
          </template>
        </PDialog>
      </div>
    `
  };

  /* ------------------------------------------------------------------ */
  /* Media picker used by the content editor                             */
  /* ------------------------------------------------------------------ */

  components.MediaPicker = {
    name: 'MediaPicker',
    props: { visible: { type: Boolean, default: false } },
    emits: ['update:visible', 'insert'],
    setup(props, { emit }) {
      const libraries = ref([]);
      const libraryId = ref(localStorage.getItem(LIBRARY_KEY) || 'local');
      const path = ref('');
      const entries = ref([]);
      const loading = ref(false);
      const error = ref('');
      const selected = ref(null);

      const crumbs = computed(() => {
        const out = [{ label: t('media.root', {}, 'Root'), path: '' }];
        let acc = '';
        String(path.value || '').split('/').filter(Boolean).forEach((segment) => {
          acc = acc ? acc + '/' + segment : segment;
          out.push({ label: segment, path: acc });
        });
        return out;
      });
      const folders = computed(() => entries.value.filter((e) => e.is_dir));
      const files = computed(() => entries.value.filter((e) => !e.is_dir));
      const currentLibrary = computed(() => libraries.value.find((l) => l.id === libraryId.value));

      async function loadLibraries() {
        try {
          const data = await HP.api('/media/libraries');
          libraries.value = (data.libraries || []).map((library) => library.builtin ? Object.assign({}, library, { name: t('media.builtin_library', {}, 'Site images') }) : library);
          if (!libraries.value.some((l) => l.id === libraryId.value)) libraryId.value = 'local';
        } catch (e) { error.value = humanError(e); }
      }
      async function load() {
        loading.value = true;
        error.value = '';
        selected.value = null;
        try {
          const data = await HP.api('/media?' + queryString({ library: libraryId.value, path: path.value }));
          entries.value = data.entries || [];
        } catch (e) { error.value = humanError(e); entries.value = []; }
        finally { loading.value = false; }
      }
      function close() { emit('update:visible', false); }
      function openFolder(entry) { path.value = entry.path; load(); }
      function goCrumb(crumb) { path.value = crumb.path; load(); }
      function selectLibrary(id) { libraryId.value = id; try { localStorage.setItem(LIBRARY_KEY, id); } catch (e) {} path.value = ''; load(); }
      function selectFile(entry) { selected.value = entry; }
      function insert(kind) {
        if (!selected.value) return;
        const entry = selected.value;
        let text = entry.url;
        if (kind === 'markdown') text = markdownFor(entry);
        if (kind === 'link') text = '[' + entry.name + '](' + entry.url + ')';
        emit('insert', { text, entry, kind });
        close();
      }

      watch(() => props.visible, (visible) => {
        if (!visible) return;
        selected.value = null;
        loadLibraries().then(load);
      });

      return {
        libraries, libraryId, path, entries, loading, error, selected, crumbs, folders, files, currentLibrary,
        close, openFolder, goCrumb, selectLibrary, selectFile, insert, kindIcon, kindLabel, formatBytes: HP.formatBytes, t
      };
    },
    template: `
      <PDialog :visible="visible" @update:visible="close" modal :header="t('media.insert_media', {}, 'Insert media')" :style="{ width: '58rem', maxWidth: '96vw' }" class="media-picker">
        <div class="media-picker-toolbar">
          <PSelect :modelValue="libraryId" :options="libraries" optionLabel="name" optionValue="id" @update:modelValue="selectLibrary" class="media-library-select" />
          <div class="media-breadcrumb">
            <template v-for="(crumb, index) in crumbs" :key="crumb.path">
              <button class="crumb" :class="{ current: index === crumbs.length - 1 }" @click="goCrumb(crumb)">{{ crumb.label }}</button>
              <i v-if="index < crumbs.length - 1" class="pi pi-angle-right"></i>
            </template>
          </div>
        </div>
        <PMessage v-if="error" severity="error" :closable="false">{{ error }}</PMessage>
        <div v-if="loading" class="media-picker-loading"><PProgressSpinner strokeWidth="3" /></div>
        <div v-else-if="entries.length === 0" class="media-picker-empty">
          <i class="pi pi-folder-open"></i>
          <span>{{ t('media.empty_title', {}, 'This folder is empty') }}</span>
        </div>
        <template v-else>
          <div v-if="folders.length" class="media-folders compact">
            <button v-for="folder in folders" :key="folder.path" class="media-folder" @click="openFolder(folder)">
              <i class="pi pi-folder"></i><span>{{ folder.name }}</span><i class="pi pi-angle-right"></i>
            </button>
          </div>
          <div class="media-picker-grid">
            <button v-for="file in files" :key="file.path" class="media-pick-item" :class="{ selected: selected && selected.path === file.path }" @click="selectFile(file)" @dblclick="selectFile(file); insert(file.kind === 'image' ? 'markdown' : 'link')">
              <span class="media-pick-thumb">
                <img v-if="file.kind === 'image'" :src="file.preview_url" :alt="file.name" loading="lazy" />
                <i v-else :class="kindIcon(file.kind)"></i>
              </span>
              <span class="media-pick-name">{{ file.name }}</span>
              <small>{{ kindLabel(file.kind) }} · {{ formatBytes(file.size) }}</small>
            </button>
          </div>
        </template>
        <template #footer>
          <span class="media-picker-selected">{{ selected ? selected.path : t('media.pick_hint', {}, 'Choose a file to insert.') }}</span>
          <PButton text :label="t('common.cancel', {}, 'Cancel')" @click="close" />
          <PButton v-if="selected && selected.kind === 'image'" text icon="pi pi-code" :label="t('media.insert_markdown', {}, 'Insert Markdown')" @click="insert('markdown')" />
          <PButton v-if="selected && selected.kind !== 'image'" text icon="pi pi-link" :label="t('media.insert_link', {}, 'Insert link')" @click="insert('link')" />
          <PButton v-if="selected" icon="pi pi-link" :label="t('media.insert_url', {}, 'Insert URL')" @click="insert('url')" />
        </template>
      </PDialog>
    `
  };
})();
