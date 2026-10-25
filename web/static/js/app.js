(function () {
  'use strict';
  const HP = window.HP;
  const PV = window.PrimeVue;
  const { onMounted } = Vue;

  const Root = {
    name: 'HugoPanelRoot',
    setup() {
      const toast = PV.useToast();
      HP.bindToast(toast);
      onMounted(() => { HP.loadInitial(); });
      function authed(data) { HP.afterLogin(data); }
      return { state: HP.state, route: HP.route, authed, retry: HP.loadInitial, t: HP.t };
    },
    template: `
      <div class="root">
        <PToast position="top-right" />
        <PConfirmDialog :style="{ width: '28rem', maxWidth: '94vw' }" />
        <div v-if="state.bootError" class="boot-screen">
          <div class="boot-card">
            <span class="brand-mark"><i class="pi pi-exclamation-triangle"></i></span>
            <h1>{{ t('boot.error_title', {}, 'Could not reach Hugo Panel') }}</h1>
            <p>{{ t('boot.error_text', {}, 'Check that the server is running and try again.') }}</p>
            <PButton :label="t('common.retry', {}, 'Retry')" icon="pi pi-refresh" @click="retry" class="btn-primary" />
          </div>
        </div>
        <div v-else-if="!state.ready" class="boot-screen">
          <div class="boot-card">
            <span class="brand-mark"><i class="pi pi-th-large"></i></span>
            <h1>Hugo Panel</h1>
            <div class="boot-loading"><span></span><span></span><span></span></div>
          </div>
        </div>
        <SetupWizard v-else-if="state.setupRequired" @done="authed" />
        <AuthView v-else-if="!state.user || route.path === '/reset'" @authed="authed" />
        <AppShell v-else />
      </div>
    `
  };

  const app = Vue.createApp(Root);

  if (PV.ToastService) app.use(PV.ToastService);
  if (PV.ConfirmationService) app.use(PV.ConfirmationService);

  const preset = PV.Themes && PV.Themes.Aura;
  const primeOptions = { ripple: true };
  if (preset) {
    primeOptions.theme = {
      preset,
      options: { darkModeSelector: '.dark', cssLayer: false }
    };
  }
  if (PV.PrimeVue) app.use(PV.PrimeVue, primeOptions);

  const primeComponents = {
    PButton: 'Button', PInputText: 'InputText', PPassword: 'Password', PDataTable: 'DataTable',
    PColumn: 'Column', PDialog: 'Dialog', PSelect: 'Select', PToggleSwitch: 'ToggleSwitch',
    PTag: 'Tag', PToast: 'Toast', PConfirmDialog: 'ConfirmDialog', PMessage: 'Message',
    PTabs: 'Tabs', PTabList: 'TabList', PTab: 'Tab', PTabPanels: 'TabPanels', PTabPanel: 'TabPanel',
    PAvatar: 'Avatar', PBadge: 'Badge', PTextarea: 'Textarea', PInputNumber: 'InputNumber',
    PDivider: 'Divider', PToolbar: 'Toolbar', PCard: 'Card', PProgressSpinner: 'ProgressSpinner',
    PSkeleton: 'Skeleton', PMenu: 'Menu', PIconField: 'IconField', PInputIcon: 'InputIcon',
    PInputGroup: 'InputGroup', PInputGroupAddon: 'InputGroupAddon', PCheckbox: 'Checkbox',
    PRadioButton: 'RadioButton', PAutoComplete: 'AutoComplete', PBreadcrumb: 'Breadcrumb',
    PDrawer: 'Drawer', PPopover: 'Popover', PSplitButton: 'SplitButton', PTimeline: 'Timeline',
    PScrollPanel: 'ScrollPanel', POverlayBadge: 'OverlayBadge'
  };
  Object.keys(primeComponents).forEach((name) => {
    const comp = PV[primeComponents[name]];
    if (comp) app.component(name, comp);
  });
  if (PV.Tooltip) app.directive('tooltip', PV.Tooltip);

  Object.keys(HP.components || {}).forEach((name) => app.component(name, HP.components[name]));

  app.config.errorHandler = (err, instance, info) => {
    console.error('[hugo-panel]', info, err);
    try { HP.toast('error', HP.t('errors.generic', {}, 'Something went wrong'), err && err.message); } catch (e) {}
  };
  app.mount('#app');
})();
