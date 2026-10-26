/* Hugo Panel — landing page behaviour. No dependencies, no build step. */
(function () {
  'use strict';

  var root = document.documentElement;
  var STORAGE_KEY = 'hp-docs-theme';

  function isDark() {
    return root.getAttribute('data-theme') === 'dark';
  }

  var meta = document.querySelector('meta[name="theme-color"]');
  function syncThemeColor() {
    if (meta) meta.setAttribute('content', isDark() ? '#000000' : '#f4f5f7');
  }
  syncThemeColor();

  var toggle = document.getElementById('theme-toggle');
  if (toggle) {
    toggle.addEventListener('click', function () {
      var next = isDark() ? 'light' : 'dark';
      root.setAttribute('data-theme', next);
      try { localStorage.setItem(STORAGE_KEY, next); } catch (e) {}
      syncThemeColor();
    });
  }

  // Until the visitor picks a theme explicitly, keep following the OS setting.
  var saved = null;
  try { saved = localStorage.getItem(STORAGE_KEY); } catch (e) {}
  var mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  if (!saved && mq) {
    var follow = function (event) {
      root.setAttribute('data-theme', event.matches ? 'dark' : 'light');
      syncThemeColor();
    };
    if (mq.addEventListener) mq.addEventListener('change', follow);
    else if (mq.addListener) mq.addListener(follow);
  }

  // Copy buttons on the quick start snippets.
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
    var label = button.textContent;
    button.addEventListener('click', function () {
      var target = document.getElementById(button.getAttribute('data-copy'));
      if (!target) return;
      copyText(target.textContent.trim()).then(function () {
        button.textContent = '已复制';
        button.classList.add('is-copied');
        setTimeout(function () {
          button.textContent = label;
          button.classList.remove('is-copied');
        }, 1600);
      }).catch(function () {
        button.textContent = '复制失败';
        setTimeout(function () { button.textContent = label; }, 1600);
      });
    });
  });
})();
