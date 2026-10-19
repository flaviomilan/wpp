import {defineConfig} from '@playwright/test';
export default defineConfig({
  testDir:'./internal/webui/tests/browser',
  fullyParallel:true,
  use:{baseURL:'http://127.0.0.1:4199',browserName:'chromium',trace:'retain-on-failure'},
  webServer:{command:'node internal/webui/tests/browser/server.mjs',url:'http://127.0.0.1:4199',reuseExistingServer:false},
  reporter:'list',
});
