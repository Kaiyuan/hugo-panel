项目名称 hugo panel，项目地址 `https://github.com/Kaiyuan/hugo-panel`
开发一个 Hugo 管理后台，需要对 Hugo 站点进行内容管理、主题配置和插件管理等功能。
所有设置都需要让用户通过一个直观的界面进行操作，而不需要直接编辑配置文件或代码。
项目使用 Go 开发，页面使用 PrimeVue，项目不能使用 nodejs。
UI 风格按照 `./docs/assets/images/ui.png` 字体至少 14px 以上，`./docs/assets/PrimeVue_llms.txt` 是 UI库说明。
项目优先使用 Docker 部署, github 每发布一个 Releases Tag 就使用 Actions 打包x64 和 ARM 镜像发布的到 `https://hub.docker.com/r/kaiyuan/hugo-panel` 。支持 简体中文，繁体中文，英文，法文，意大利文，西班牙文，日文，韩文等多国语言，并且每一种语言独立文件，需要时载入对应译文文本，同时方便用户添加自己的译文。支持亮色和暗色主题，暗色主题使用纯黑背景，因为 OLED 屏幕纯黑的像素不发光十分省电。
项目使用 SQLite 作为数据库，初次启动时候如果还没有用户则提醒用户设置管理员名称和密码，项目允许用户配置 hugo 目录，Docker 运行的时候，项目目录设置为 `/app/hugo-panel`，hugo 目录默认设置为 `/app/hugo`。
项目允许用户在 Docker 启动或项目设置页面中设置 SMTP 邮箱设置，用户找回密码和发送通知等。
项目支持 PWA，需要针对移动设备优化。