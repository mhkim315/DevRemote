// 9.5-R1: Expo config plugin that injects android:usesCleartextTraffic="true"
// into the generated AndroidManifest.xml. Required by Pairing V1 LAN HTTP /pair.
const { withAndroidManifest } = require('@expo/config-plugins');

function withCleartextTraffic(config) {
  return withAndroidManifest(config, (cfg) => {
    const app = cfg.modResults.manifest.application;
    if (app && app.length > 0) {
      // android:usesCleartextTraffic must be a string "true" in the manifest
      app[0].$['android:usesCleartextTraffic'] = 'true';
    }
    return cfg;
  });
}

module.exports = withCleartextTraffic;
