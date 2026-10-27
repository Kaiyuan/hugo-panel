/* Hugo Panel service worker. The server replaces __PANEL_VERSION__ with the panel version and asset fingerprint. */
const VERSION = '__PANEL_VERSION__';
const CACHE = 'hugo-panel-' + VERSION;
const I18N_CACHE = 'hugo-panel-i18n-' + VERSION;

const PRECACHE = [
  '/',
  '/index.html',
  '/manifest.webmanifest',
  '/static/css/app.css',
  '/static/js/boot.js',
  '/static/js/core.js',
  '/static/js/components-ui.js',
  '/static/js/pages-dashboard.js',
  '/static/js/pages-content.js',
  '/static/js/media.js',
  '/static/js/pages-media-themes.js',
  '/static/js/pages-plugins-build.js',
  '/static/js/pages-settings.js',
  '/static/js/app.js',
  '/static/vendor/vue.global.prod.js',
  '/static/vendor/primevue.umd.min.js',
  '/static/vendor/primevue-themes-aura.umd.min.js',
  '/static/vendor/marked.umd.js',
  '/static/vendor/dompurify.min.js',
  '/static/vendor/primeicons.css',
  '/static/vendor/fonts/primeicons.woff2',
  '/static/icons/favicon.svg',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png',
  '/static/icons/maskable-512.png',
  '/static/icons/apple-touch-icon.png'
];

async function precache() {
  const cache = await caches.open(CACHE);
  await Promise.all(PRECACHE.map(async (url) => {
    const response = await fetch(new Request(url, { cache: 'reload' }));
    if (!response || !response.ok) throw new Error('Failed to precache ' + url);
    await cache.put(url, response);
  }));
}

self.addEventListener('install', (event) => {
  event.waitUntil(precache().then(() => self.skipWaiting()));
});

self.addEventListener('message', (event) => {
  if (event.data === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => Promise.all(keys.map((key) => {
      if (key !== CACHE && key !== I18N_CACHE) return caches.delete(key);
      return null;
    }))).then(() => self.clients.claim())
  );
});

async function networkFirstNavigation(request) {
  try {
    const response = await fetch(request);
    return response;
  } catch (e) {
    const cache = await caches.open(CACHE);
    return (await cache.match('/index.html')) || (await cache.match('/')) || Response.error();
  }
}

async function cacheFirst(request) {
  const cache = await caches.open(CACHE);
  const cached = await cache.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response && response.ok && response.type === 'basic') {
    cache.put(request, response.clone()).catch(() => {});
  }
  return response;
}

async function networkFirstAsset(request) {
  const cache = await caches.open(CACHE);
  try {
    const response = await fetch(request);
    if (response && response.ok && response.type === 'basic') {
      cache.put(request, response.clone()).catch(() => {});
    }
    return response;
  } catch (e) {
    const cached = await cache.match(request);
    return cached || Response.error();
  }
}

async function i18nStaleWhileRevalidate(request) {
  const cache = await caches.open(I18N_CACHE);
  const cached = await cache.match(request);
  const update = fetch(request).then((response) => {
    if (response && response.ok) cache.put(request, response.clone()).catch(() => {});
    return response;
  }).catch(() => null);
  return cached || (await update) || Response.error();
}

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname === '/sw.js') return;
  if (url.pathname.startsWith('/api/')) {
    if (url.pathname.startsWith('/api/i18n/')) {
      event.respondWith(i18nStaleWhileRevalidate(request));
    }
    return;
  }
  if (request.mode === 'navigate') {
    event.respondWith(networkFirstNavigation(request));
    return;
  }
  if (url.pathname.startsWith('/static/vendor/') || url.pathname.startsWith('/static/icons/')) {
    event.respondWith(cacheFirst(request));
    return;
  }
  if (url.pathname.startsWith('/static/') || url.pathname === '/manifest.webmanifest') {
    event.respondWith(networkFirstAsset(request));
    return;
  }
  if (url.pathname === '/' || url.pathname === '/index.html') {
    event.respondWith(networkFirstNavigation(request));
  }
});
