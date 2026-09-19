/**
 * Nginx Online Web Builder - Pluggable i18n Loader & Engine
 * Loads locale definitions from dedicated JSON packs in `/locales/<lang>.json`
 * Supports dynamic subpath base URL resolution (e.g. /nginx or root /)
 */

function getAppBasePath() {
  if (typeof window !== "undefined" && typeof window.__BASE_PATH__ === "string") {
    return window.__BASE_PATH__.replace(/\/+$/, "");
  }
  const meta = typeof document !== "undefined" ? document.querySelector('meta[name="base-path"]') : null;
  if (meta && meta.content) {
    return meta.content.replace(/\/+$/, "");
  }
  let p = (typeof window !== "undefined" && window.location.pathname) ? window.location.pathname : "/";
  p = p.replace(/\/[^\/]*\.[a-zA-Z0-9]+$/, ""); // strip filename like index.html
  p = p.replace(/\/+$/, "");
  return p;
}

function getAuthToken() {
  if (typeof window === "undefined") return "";
  try {
    const params = new URLSearchParams(window.location.search);
    const qKey = params.get("key") || params.get("token") || params.get("auth");
    if (qKey) {
      localStorage.setItem("nginx_builder_key", qKey);
      return qKey;
    }
    return localStorage.getItem("nginx_builder_key") || "";
  } catch (e) {
    return "";
  }
}

function apiUrl(path) {
  if (!path) return path;
  if (path.startsWith("http://") || path.startsWith("https://")) return path;
  const base = getAppBasePath();
  const clean = path.startsWith("/") ? path : "/" + path;
  const alreadyPrefixed = base && (clean === base || clean.startsWith(base + "/"));
  let full = base && !alreadyPrefixed ? (base + clean) : clean;

  // If token is present, append to API and locales URLs as secondary fallback
  const token = getAuthToken();
  if (token && (full.includes("/api/") || full.includes("/locales/"))) {
    const sep = full.includes("?") ? "&" : "?";
    if (!full.includes("key=") && !full.includes("token=") && !full.includes("auth=")) {
      full += sep + "key=" + encodeURIComponent(token);
    }
  }
  return full;
}

window.getAppBasePath = getAppBasePath;
window.getAuthToken = getAuthToken;
window.apiUrl = apiUrl;

const I18N_ENGINE = {
  currentLang: "en",
  languages: [],
  loadedPacks: {},
  fallbackPack: null,

  /**
   * Determine initial active language:
   * 1. localStorage saved preference
   * 2. navigator.language matching supported codes
   * 3. Default fallback: 'en'
   */
  getInitialLang() {
    const saved = localStorage.getItem("nginx_builder_lang");
    if (saved) return saved;

    const navLang = (navigator.language || navigator.userLanguage || "").toLowerCase();
    if (navLang.startsWith("zh")) {
      return "zh";
    }
    return "en";
  },

  /**
   * Initialize i18n subsystem: load languages manifest and initial language pack
   */
  async init() {
    this.currentLang = this.getInitialLang();

    try {
      // 1. Load languages manifest
      const langResp = await fetch(apiUrl("/locales/languages.json"));
      if (langResp.ok) {
        this.languages = await langResp.json();
      } else {
        this.languages = [
          { code: "en", name: "English", default: true },
          { code: "zh", name: "简体中文" }
        ];
      }
    } catch (e) {
      this.languages = [
        { code: "en", name: "English", default: true },
        { code: "zh", name: "简体中文" }
      ];
    }

    // Populate language switcher dropdown dynamically
    this.populateLanguageSwitcher();

    // 2. Load fallback English pack first
    await this.loadPack("en");
    this.fallbackPack = this.loadedPacks["en"];

    // 3. Load active language pack if different
    if (this.currentLang !== "en") {
      await this.loadPack(this.currentLang);
    }

    this.apply();
  },

  /**
   * Populate #lang-select dropdown with registered languages
   */
  populateLanguageSwitcher() {
    const select = document.getElementById("lang-select");
    if (!select) return;

    select.innerHTML = "";
    this.languages.forEach(l => {
      const opt = document.createElement("option");
      opt.value = l.code;
      opt.textContent = l.name;
      if (l.code === this.currentLang) {
        opt.selected = true;
      }
      select.appendChild(opt);
    });

    select.onchange = (e) => {
      this.switchLanguage(e.target.value);
    };
  },

  /**
   * Dynamically fetch a language pack from `/locales/<code>.json`
   */
  async loadPack(code) {
    if (this.loadedPacks[code]) return this.loadedPacks[code];

    try {
      const resp = await fetch(apiUrl(`/locales/${code}.json`));
      if (resp.ok) {
        const data = await resp.json();
        this.loadedPacks[code] = data;
        return data;
      }
    } catch (err) {
      console.warn(`Failed to load locale pack for '${code}':`, err);
    }

    return null;
  },

  /**
   * Switch language at runtime, load pack if necessary, update DOM and persist
   */
  async switchLanguage(code) {
    if (code === this.currentLang && this.loadedPacks[code]) return;

    await this.loadPack(code);
    this.currentLang = code;
    localStorage.setItem("nginx_builder_lang", code);
    document.documentElement.lang = code === "zh" ? "zh-CN" : code;

    const select = document.getElementById("lang-select");
    if (select && select.value !== code) {
      select.value = code;
    }

    this.apply();

    // Notify app components to re-render dynamic content
    if (typeof renderTabs === "function") renderTabs();
    if (typeof renderOptionsGrid === "function") renderOptionsGrid();
    if (typeof renderPathsGrid === "function") renderPathsGrid();
    if (typeof updateDepsBadge === "function") updateDepsBadge();
    if (typeof triggerPreview === "function") triggerPreview();
  },

  /**
   * Retrieve translated string by key
   */
  t(key, fallback = "") {
    const pack = this.loadedPacks[this.currentLang] || this.fallbackPack || {};
    if (pack[key] !== undefined) return pack[key];
    if (this.fallbackPack && this.fallbackPack[key] !== undefined) return this.fallbackPack[key];
    return fallback || key;
  },

  /**
   * Retrieve option description
   */
  getOptionDesc(id, defaultDesc = "") {
    const pack = this.loadedPacks[this.currentLang] || this.fallbackPack || {};
    if (pack.options && pack.options[id] !== undefined) return pack.options[id];
    if (this.fallbackPack && this.fallbackPack.options && this.fallbackPack.options[id] !== undefined) {
      return this.fallbackPack.options[id];
    }
    return defaultDesc;
  },

  /**
   * Retrieve localized category name
   */
  getCategoryName(categoryKey) {
    const keyMap = {
      "SSL/TLS": "cat_ssl",
      "HTTP": "cat_http",
      "Stream": "cat_stream",
      "性能": "cat_performance",
      "Performance": "cat_performance",
      "Mail": "cat_mail",
      "调试": "cat_debug",
      "Debug": "cat_debug",
      "系统相关": "cat_system",
      "System": "cat_system",
      "其他官方模块": "cat_other",
      "Other Official Modules": "cat_other"
    };
    const i18nKey = keyMap[categoryKey];
    return i18nKey ? this.t(i18nKey, categoryKey) : categoryKey;
  },

  /**
   * Retrieve localized path description
   */
  getPathDesc(key, defaultDesc = "") {
    const pack = this.loadedPacks[this.currentLang] || this.fallbackPack || {};
    if (pack.paths && pack.paths[key] !== undefined) return pack.paths[key];
    if (this.fallbackPack && this.fallbackPack.paths && this.fallbackPack.paths[key] !== undefined) {
      return this.fallbackPack.paths[key];
    }
    return defaultDesc;
  },

  /**
   * Apply translations to all DOM elements with data-i18n attributes
   */
  apply() {
    document.querySelectorAll("[data-i18n]").forEach(el => {
      const key = el.getAttribute("data-i18n");
      const val = this.t(key);
      if (val) el.textContent = val;
    });

    document.querySelectorAll("[data-i18n-html]").forEach(el => {
      const key = el.getAttribute("data-i18n-html");
      const val = this.t(key);
      if (val) el.innerHTML = val;
    });

    document.querySelectorAll("[data-i18n-placeholder]").forEach(el => {
      const key = el.getAttribute("data-i18n-placeholder");
      const val = this.t(key);
      if (val) el.setAttribute("placeholder", val);
    });
  }
};

// Global helper wrappers compatible with existing codebase
window.I18N_ENGINE = I18N_ENGINE;
window.t = (key, fallback) => I18N_ENGINE.t(key, fallback);
window.getOptionDesc = (id, fallback) => I18N_ENGINE.getOptionDesc(id, fallback);
window.getCategoryName = (cat) => I18N_ENGINE.getCategoryName(cat);
window.getPathDesc = (key, fallback) => I18N_ENGINE.getPathDesc(key, fallback);
window.setLanguage = (lang) => I18N_ENGINE.switchLanguage(lang);
Object.defineProperty(window, 'currentLang', {
  get() { return I18N_ENGINE.currentLang; },
  set(val) { I18N_ENGINE.currentLang = val; }
});
