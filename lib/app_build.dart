const liteEnabled = bool.fromEnvironment('LITE');
const allSourcesEnabled = liteEnabled || bool.fromEnvironment('ALL_SOURCES');
const appName = liteEnabled ? '真果鉴 Lite' : allSourcesEnabled ? '真果鉴' : '红果鉴';
const appSlug = liteEnabled ? 'zhenguojian-lite' : allSourcesEnabled ? 'zhenguojian' : 'hongguojian';
