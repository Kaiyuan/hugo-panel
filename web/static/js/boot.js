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
})();
