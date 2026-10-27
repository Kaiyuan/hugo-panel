/* Hugo Panel — landing page behaviour. No dependencies, no build step. */
(function () {
  'use strict';

  var DEFAULT_LANG = 'zh-CN';
  var SUPPORTED = window.__HP_DOCS_SUPPORTED__ || ['zh-CN', 'zh-TW', 'en', 'fr', 'it', 'es', 'ja', 'ko', 'de'];
  var LANG_KEY = 'hp-docs-lang';
  var STORAGE_KEY = 'hp-docs-theme';

  var root = document.documentElement;
  var langSelect = document.getElementById('lang-select');
  var metaTheme = document.querySelector('meta[name="theme-color"]');
  var metaDescription = document.querySelector('meta[name="description"]');
  var dict = {};

  var defaultTitle = document.title;
  var defaultDescription = metaDescription ? metaDescription.getAttribute('content') : '';

  function each(selector, fn) {
    Array.prototype.forEach.call(document.querySelectorAll(selector), fn);
  }

  function tr(key, fallback) {
    return dict[key] != null ? dict[key] : fallback;
  }

  /* ---------- Theme ---------- */
  function isDark() {
    return root.getAttribute('data-theme') === 'dark';
  }

  function syncThemeColor() {
    if (metaTheme) metaTheme.setAttribute('content', isDark() ? '#000000' : '#f4f5f7');
  }
  syncThemeColor();

  var themeToggle = document.getElementById('theme-toggle');
  if (themeToggle) {
    themeToggle.addEventListener('click', function () {
      var next = isDark() ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem(STORAGE_KEY, next); } catch (e) {}
      syncThemeColor();
    });
  }

  // Until the visitor picks a theme explicitly, keep following the OS setting.
  var savedTheme = null;
  try { savedTheme = localStorage.getItem(STORAGE_KEY); } catch (e) {}
  var mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  if (!savedTheme && mq) {
    var follow = function (event) {
      root.setAttribute('data-theme', event.matches ? 'dark' : 'light');
      syncThemeColor();
    };
    if (mq.addEventListener) mq.addEventListener('change', follow);
    else if (mq.addListener) mq.addListener(follow);
  }

  /* ---------- Language ---------- */
  // Keep the markup's own text as the fallback for incomplete dictionaries.
  each('[data-i18n]', function (el) {
    el.setAttribute('data-i18n-default', el.textContent);
  });
  each('[data-i18n-html]', function (el) {
    el.setAttribute('data-i18n-html-default', el.innerHTML);
  });
  each('[data-i18n-alt]', function (el) {
    el.setAttribute('data-i18n-alt-default', el.getAttribute('alt') || '');
  });
  each('[data-i18n-aria]', function (el) {
    el.setAttribute('data-i18n-aria-default', el.getAttribute('aria-label') || '');
  });

  function applyLocale(lang, data) {
    dict = data || {};

    root.lang = lang;
    root.setAttribute('data-lang', lang);
    if (langSelect) langSelect.value = lang;

    each('[data-i18n]', function (el) {
      var value = dict[el.getAttribute('data-i18n')];
      el.textContent = value != null ? value : el.getAttribute('data-i18n-default');
    });
    each('[data-i18n-html]', function (el) {
      var value = dict[el.getAttribute('data-i18n-html')];
      el.innerHTML = value != null ? value : el.getAttribute('data-i18n-html-default');
    });
    each('[data-i18n-alt]', function (el) {
      var value = dict[el.getAttribute('data-i18n-alt')];
      el.setAttribute('alt', value != null ? value : el.getAttribute('data-i18n-alt-default'));
    });
    each('[data-i18n-aria]', function (el) {
      var value = dict[el.getAttribute('data-i18n-aria')];
      el.setAttribute('aria-label', value != null ? value : el.getAttribute('data-i18n-aria-default'));
    });

    document.title = dict['page.title'] || defaultTitle;
    if (metaDescription) metaDescription.setAttribute('content', dict['page.description'] || defaultDescription);

    root.classList.remove('i18n-pending');
  }

  function loadLocale(lang) {
    if (SUPPORTED.indexOf(lang) < 0) lang = DEFAULT_LANG;
    if (!window.fetch) {
      applyLocale(DEFAULT_LANG, null);
      return Promise.resolve(DEFAULT_LANG);
    }
    return fetch('assets/locales/' + lang + '.json', { cache: 'no-cache' })
      .then(function (response) {
        if (!response.ok) throw new Error('HTTP ' + response.status);
        return response.json();
      })
      .then(function (data) {
        applyLocale(lang, data);
        return lang;
      })
      .catch(function () {
        // Missing or broken dictionary: fall back to the markup's own language.
        applyLocale(DEFAULT_LANG, null);
        return DEFAULT_LANG;
      });
  }

  function setLanguage(lang, persist) {
    if (SUPPORTED.indexOf(lang) < 0) lang = DEFAULT_LANG;
    return loadLocale(lang).then(function (effective) {
      if (!persist) return effective;
      try { localStorage.setItem(LANG_KEY, effective); } catch (e) {}
      try {
        var url = new URL(window.location.href);
        if (effective === DEFAULT_LANG) url.searchParams.delete('lang');
        else url.searchParams.set('lang', effective);
        window.history.replaceState(null, '', url);
      } catch (e) {}
      return effective;
    });
  }

  if (langSelect) {
    langSelect.addEventListener('change', function () {
      setLanguage(langSelect.value, true);
    });
  }

  setLanguage(window.__HP_DOCS_LANG__ || DEFAULT_LANG, false);

  /* ---------- Copy buttons ---------- */
  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function (resolve, reject) {
      var area = document.createElement('textarea');
      area.value = text;
      area.setAttribute('readonly', '');
      area.style.position = 'fixed';
      area.style.top = '-1000px';
      document.body.appendChild(area);
      area.select();
      try {
        document.execCommand('copy') ? resolve() : reject(new Error('copy failed'));
      } catch (e) {
        reject(e);
      } finally {
        document.body.removeChild(area);
      }
    });
  }

  Array.prototype.forEach.call(document.querySelectorAll('.copy-btn'), function (button) {
    button.addEventListener('click', function () {
      var target = document.getElementById(button.getAttribute('data-copy'));
      if (!target) return;
      var idleLabel = tr('start.copy', button.getAttribute('data-i18n-default') || '复制');
      copyText(target.textContent.trim()).then(function () {
        button.textContent = tr('start.copied', '已复制');
        button.classList.add('is-copied');
        setTimeout(function () {
          button.textContent = idleLabel;
          button.classList.remove('is-copied');
        }, 1600);
      }).catch(function () {
        button.textContent = tr('start.copyFailed', '复制失败');
        setTimeout(function () { button.textContent = idleLabel; }, 1600);
      });
    });
  });
})();
