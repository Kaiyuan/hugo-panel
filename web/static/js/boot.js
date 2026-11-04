(function () {
  'use strict';
  try {
    var mode = localStorage.getItem('hp_theme') || 'system';
    var dark = mode === 'dark' || (mode === 'system' && window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches);
    if (dark) document.documentElement.classList.add('dark');
    document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
    var lang = localStorage.getItem('hp_lang');
    if (lang) document.documentElement.lang = lang;
  } catch (e) {}

  // Mobile devices must not zoom the panel. The viewport meta covers most
  // browsers and touch-action covers Android; Safari ignores user-scalable=no,
  // so block its pinch gesture and multi-touch moves explicitly.
  if (navigator.maxTouchPoints > 0 || 'ontouchstart' in window) {
    var blockZoom = function (event) {
      if (event.cancelable !== false) event.preventDefault();
    };
    ['gesturestart', 'gesturechange', 'gestureend'].forEach(function (name) {
      document.addEventListener(name, blockZoom, { passive: false });
    });
    document.addEventListener('touchmove', function (event) {
      if (event.touches && event.touches.length > 1 && event.cancelable !== false) event.preventDefault();
    }, { passive: false });
  }
})();
