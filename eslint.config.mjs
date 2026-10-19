export default [{
  files: ["**/*.js", "**/*.mjs"],
  languageOptions: {
    ecmaVersion: "latest",
    sourceType: "module",
    globals: Object.fromEntries(["document", "window", "navigator", "globalThis", "URL", "URLSearchParams", "console", "setInterval", "clearInterval", "setTimeout", "clearTimeout", "process", "Buffer", "structuredClone"].map(name => [name, "readonly"]))
  },
  rules: {
    "no-unused-vars": ["error", {"args": "after-used", "caughtErrors": "none"}],
    "no-undef": "error",
    "no-unreachable": "error",
    "no-constant-condition": "error",
    "no-dupe-keys": "error",
    "no-duplicate-imports": "error",
    "no-eval": "error",
    "valid-typeof": "error"
  }
}];
