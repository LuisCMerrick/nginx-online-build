// Nginx Online Builder Client Logic
let allOptions = [];
let optionCategories = {};
let pathDefaults = {};
let depLibraries = {};
let selectedOptionIds = new Set();
let pathOverrides = {};
let activeTab = "SSL/TLS";
let activeBuildId = null;
let logEventSource = null;
let pollTimer = null;

// Third-party source dependencies state
let thirdPartySources = {
  use_openssl_source: false,
  openssl_version: "3.4.1",
  openssl_source_url: "",
  openssl_opt: "",

  use_pcre_source: false,
  pcre_version: "10.45",
  pcre_source_url: "",
  pcre_opt: "",

  use_zlib_source: false,
  zlib_version: "1.3.1",
  zlib_source_url: "",
  zlib_opt: ""
};

// Initial preset recommendations for typical modern web / proxy usage
const recommendedPresets = [
  "http_ssl",
  "http_v2",
  "stream",
  "stream_ssl",
  "http_realip",
  "http_gzip_static",
  "http_stub_status",
  "pcre_jit",
  "threads"
];

document.addEventListener("DOMContentLoaded", async () => {
  if (window.I18N_ENGINE && typeof window.I18N_ENGINE.init === "function") {
    await window.I18N_ENGINE.init();
  } else if (typeof applyStaticI18n === "function") {
    applyStaticI18n();
  }
  initEventListeners();
  initDepListeners();
  loadVersions();
  loadOptions();
  loadRecentBuilds();
  loadSystemStatus();
});

function initEventListeners() {
  // Language switcher
  const langSel = document.getElementById("lang-select");
  if (langSel) {
    langSel.value = currentLang;
    langSel.addEventListener("change", (e) => {
      setLanguage(e.target.value);
    });
  }

  // Tabs click delegation
  document.getElementById("category-tabs").addEventListener("click", (e) => {
    const tab = e.target.closest(".tab-item");
    if (!tab) return;
    document.querySelectorAll(".tab-item").forEach(t => t.classList.remove("active"));
    tab.classList.add("active");
    activeTab = tab.dataset.category;
    renderOptionsGrid();
  });

  // Version change
  document.getElementById("version-select").addEventListener("change", (e) => {
    updateVersionMeta(e.target.value);
    triggerPreview();
  });

  // Buttons
  const btnPresets = document.getElementById("btn-presets-modal") || document.getElementById("btn-select-all");
  if (btnPresets) {
    btnPresets.addEventListener("click", () => {
      openPresetsModal();
    });
  }

  const presetQuickSel = document.getElementById("preset-template-select");
  if (presetQuickSel) {
    presetQuickSel.addEventListener("change", (e) => {
      applyPresetTemplate(e.target.value);
    });
  }

  document.getElementById("btn-reset-default").addEventListener("click", () => {
    selectedOptionIds.clear();
    allOptions.forEach(opt => {
      if (opt.default_state) {
        selectedOptionIds.add(opt.id);
      }
    });
    renderOptionsGrid();
    triggerPreview();
  });

  // Toggle Advanced Paths
  document.getElementById("toggle-paths").addEventListener("click", () => {
    const card = document.getElementById("paths-card");
    card.classList.toggle("collapsed");
    const icon = card.querySelector(".toggle-icon");
    icon.textContent = card.classList.contains("collapsed") ? "▼" : "▲";
  });

  // Copy Preview
  document.getElementById("copy-preview-btn").addEventListener("click", () => {
    const text = document.getElementById("configure-preview").textContent;
    navigator.clipboard.writeText(text).then(() => {
      const btn = document.getElementById("copy-preview-btn");
      btn.textContent = t("copied", "Copied!");
      setTimeout(() => { btn.textContent = t("btn_copy", "Copy"); }, 2000);
    });
  });

  // Start Build
  document.getElementById("start-build-btn").addEventListener("click", startBuild);

  // Terminal Controls
  document.getElementById("clear-term-btn").addEventListener("click", () => {
    document.getElementById("terminal-body").textContent = "";
  });

  // Refresh history
  document.getElementById("btn-refresh-history").addEventListener("click", loadRecentBuilds);
}

function initDepListeners() {
  // 1. OpenSSL Checkbox & Inputs
  const chkOpenSSL = document.getElementById("enable-openssl-src");
  const panelOpenSSL = document.getElementById("openssl-options-panel");
  const selOpenSSLVer = document.getElementById("openssl-version-select");
  const groupOpenSSLCustom = document.getElementById("openssl-custom-url-group");
  const inputOpenSSLCustom = document.getElementById("openssl-custom-url");
  const inputOpenSSLOpt = document.getElementById("openssl-opt");

  chkOpenSSL.addEventListener("change", () => {
    thirdPartySources.use_openssl_source = chkOpenSSL.checked;
    panelOpenSSL.classList.toggle("hidden", !chkOpenSSL.checked);
    updateDepsBadge();
    triggerPreview();
  });

  selOpenSSLVer.addEventListener("change", () => {
    const isCustom = selOpenSSLVer.value === "custom";
    groupOpenSSLCustom.classList.toggle("hidden", !isCustom);
    thirdPartySources.openssl_version = selOpenSSLVer.value;
    triggerPreview();
  });

  inputOpenSSLCustom.addEventListener("input", () => {
    thirdPartySources.openssl_source_url = inputOpenSSLCustom.value.trim();
    triggerPreview();
  });

  inputOpenSSLOpt.addEventListener("input", () => {
    thirdPartySources.openssl_opt = inputOpenSSLOpt.value.trim();
    triggerPreview();
  });

  // 2. PCRE Checkbox & Inputs
  const chkPCRE = document.getElementById("enable-pcre-src");
  const panelPCRE = document.getElementById("pcre-options-panel");
  const selPCREVer = document.getElementById("pcre-version-select");
  const groupPCRECustom = document.getElementById("pcre-custom-url-group");
  const inputPCRECustom = document.getElementById("pcre-custom-url");
  const inputPCREOpt = document.getElementById("pcre-opt");

  chkPCRE.addEventListener("change", () => {
    thirdPartySources.use_pcre_source = chkPCRE.checked;
    panelPCRE.classList.toggle("hidden", !chkPCRE.checked);
    updateDepsBadge();
    triggerPreview();
  });

  selPCREVer.addEventListener("change", () => {
    const isCustom = selPCREVer.value === "custom";
    groupPCRECustom.classList.toggle("hidden", !isCustom);
    thirdPartySources.pcre_version = selPCREVer.value;
    triggerPreview();
  });

  inputPCRECustom.addEventListener("input", () => {
    thirdPartySources.pcre_source_url = inputPCRECustom.value.trim();
    triggerPreview();
  });

  inputPCREOpt.addEventListener("input", () => {
    thirdPartySources.pcre_opt = inputPCREOpt.value.trim();
    triggerPreview();
  });

  // 3. zlib Checkbox & Inputs
  const chkZlib = document.getElementById("enable-zlib-src");
  const panelZlib = document.getElementById("zlib-options-panel");
  const selZlibVer = document.getElementById("zlib-version-select");
  const groupZlibCustom = document.getElementById("zlib-custom-url-group");
  const inputZlibCustom = document.getElementById("zlib-custom-url");
  const inputZlibOpt = document.getElementById("zlib-opt");

  chkZlib.addEventListener("change", () => {
    thirdPartySources.use_zlib_source = chkZlib.checked;
    panelZlib.classList.toggle("hidden", !chkZlib.checked);
    updateDepsBadge();
    triggerPreview();
  });

  selZlibVer.addEventListener("change", () => {
    const isCustom = selZlibVer.value === "custom";
    groupZlibCustom.classList.toggle("hidden", !isCustom);
    thirdPartySources.zlib_version = selZlibVer.value;
    triggerPreview();
  });

  inputZlibCustom.addEventListener("input", () => {
    thirdPartySources.zlib_source_url = inputZlibCustom.value.trim();
    triggerPreview();
  });

  inputZlibOpt.addEventListener("input", () => {
    thirdPartySources.zlib_opt = inputZlibOpt.value.trim();
    triggerPreview();
  });
}

function updateDepsBadge() {
  const badge = document.getElementById("deps-status-badge");
  const activeDeps = [];
  if (thirdPartySources.use_openssl_source) activeDeps.push("OpenSSL");
  if (thirdPartySources.use_pcre_source) activeDeps.push("PCRE");
  if (thirdPartySources.use_zlib_source) activeDeps.push("zlib");

  if (activeDeps.length > 0) {
    badge.textContent = `${t("static_embed_badge", "Statically Embedded:")} ${activeDeps.join(" + ")}`;
    badge.style.background = "rgba(0, 150, 57, 0.2)";
    badge.style.color = "#34d399";
    badge.style.border = "1px solid rgba(0, 150, 57, 0.4)";
  } else {
    badge.textContent = t("system_libs_badge", "System Shared Libraries");
    badge.style.background = "var(--bg-input)";
    badge.style.color = "var(--text-secondary)";
    badge.style.border = "1px solid var(--border-color)";
  }
}

// Load Versions
async function loadVersions() {
  try {
    const resp = await fetch(window.apiUrl("/api/nginx/versions"));
    const data = await resp.json();
    if (!data.success || !data.versions) return;

    const select = document.getElementById("version-select");
    select.innerHTML = "";

    window.nginxVersions = data.versions;
    let defaultVer = "1.30.5";

    data.versions.forEach(v => {
      const opt = document.createElement("option");
      opt.value = v.version;
      let label = `${v.version} (${v.channel})`;
      if (v.is_default) {
        label += ` ${t("tag_recommended", "★ Recommended")}`;
        defaultVer = v.version;
      }
      opt.textContent = label;
      select.appendChild(opt);
    });

    select.value = defaultVer;
    document.getElementById("current-version-badge").textContent = `Nginx ${defaultVer}`;
    updateVersionMeta(defaultVer);
  } catch (err) {
    console.error("加载版本列表失败:", err);
  }
}

function updateVersionMeta(ver) {
  if (!window.nginxVersions) return;
  const item = window.nginxVersions.find(v => v.version === ver);
  if (item) {
    document.getElementById("meta-channel").textContent = item.channel;
    document.getElementById("meta-sha256").textContent = item.expected_sha256 || "Auto-calculated";
  }
}

// Load Options
async function loadOptions() {
  try {
    const resp = await fetch(window.apiUrl("/api/nginx/options"));
    const data = await resp.json();
    if (!data.success) return;

    allOptions = data.options || [];
    optionCategories = data.categories || {};
    pathDefaults = data.path_defaults || {};
    depLibraries = data.dep_libraries || {};

    if (data.presets && Array.isArray(data.presets)) {
      window.parameterPresets = data.presets;
    }
    renderPresetsUI();

    // Populate initial recommended modules
    applyPresetTemplate("modern_web", false);

    renderTabs();
    renderOptionsGrid();
    renderPathsGrid();
    triggerPreview();
  } catch (err) {
    console.error("加载参数失败:", err);
  }
}

function renderTabs() {
  const tabsContainer = document.getElementById("category-tabs");
  tabsContainer.innerHTML = "";

  const order = ["SSL/TLS", "HTTP", "Stream", "性能", "Performance", "Mail", "调试", "Debug", "系统相关", "System", "其他官方模块", "Other Official Modules"];
  const categories = Object.keys(optionCategories);
  const sorted = categories.sort((a, b) => {
    let idxA = order.indexOf(a);
    let idxB = order.indexOf(b);
    if (idxA === -1) idxA = 99;
    if (idxB === -1) idxB = 99;
    return idxA - idxB;
  });

  sorted.forEach((cat) => {
    const tab = document.createElement("div");
    tab.className = `tab-item ${cat === activeTab ? "active" : ""}`;
    tab.dataset.category = cat;
    tab.textContent = `${getCategoryName(cat)} (${optionCategories[cat].length})`;
    tabsContainer.appendChild(tab);
  });
}

function renderOptionsGrid() {
  const container = document.getElementById("options-container");
  container.innerHTML = "";

  const items = optionCategories[activeTab] || [];
  const grid = document.createElement("div");
  grid.className = "options-grid";

  items.forEach(opt => {
    const isChecked = selectedOptionIds.has(opt.id);
    const itemEl = document.createElement("div");
    itemEl.className = `option-item ${isChecked ? "selected" : ""}`;

    const desc = getOptionDesc(opt.id, opt.description);
    const reqLabel = t("dep_requires", "Requires");

    itemEl.innerHTML = `
      <input type="checkbox" id="chk-${opt.id}" ${isChecked ? "checked" : ""}>
      <div class="option-content">
        <span class="option-flag">${opt.name}</span>
        <div class="option-desc">${desc}</div>
        ${opt.requires_lib ? `<span class="option-badge">${reqLabel}: ${opt.requires_lib}</span>` : ""}
      </div>
    `;

    const chk = itemEl.querySelector("input");
    chk.addEventListener("change", (e) => {
      e.stopPropagation();
      toggleOption(opt.id, chk.checked);
    });

    itemEl.addEventListener("click", () => {
      chk.checked = !chk.checked;
      toggleOption(opt.id, chk.checked);
    });

    grid.appendChild(itemEl);
  });

  container.appendChild(grid);
}

function toggleOption(id, enable) {
  const opt = allOptions.find(o => o.id === id);

  if (enable) {
    selectedOptionIds.add(id);

    if (opt) {
      // Auto-satisfy parent dependencies
      if (opt.depends_on) {
        opt.depends_on.forEach(dep => {
          selectedOptionIds.add(dep);
        });
      }

      // Special rule: if without_pcre is checked, auto-add without_http_rewrite
      if (id === "without_pcre") {
        selectedOptionIds.add("without_http_rewrite");
      }
    }
  } else {
    selectedOptionIds.delete(id);

    // Cascade uncheck: when parent is unchecked, child options that depend on it must be unchecked
    allOptions.forEach(other => {
      if (other.depends_on && other.depends_on.includes(id) && selectedOptionIds.has(other.id)) {
        selectedOptionIds.delete(other.id);
      }
    });
  }

  renderOptionsGrid();
  triggerPreview();
}

function renderPathsGrid() {
  const container = document.getElementById("paths-body");
  container.innerHTML = "";

  Object.entries(pathDefaults).forEach(([key, meta]) => {
    const desc = getPathDesc(key, meta.description);
    const div = document.createElement("div");
    div.className = "path-input-group";
    div.innerHTML = `
      <label for="path-${key}">${meta.flag} (${desc})</label>
      <input type="text" class="form-input" id="path-${key}" placeholder="${meta.default}" value="${meta.default}">
    `;

    div.querySelector("input").addEventListener("input", (e) => {
      const val = e.target.value.trim();
      if (val && val !== meta.default) {
        pathOverrides[key] = val;
      } else {
        delete pathOverrides[key];
      }
      triggerPreview();
    });

    container.appendChild(div);
  });
}

// Preview Configure Command
let previewDebounce = null;
let currentConflicts = [];

function triggerPreview() {
  clearTimeout(previewDebounce);
  previewDebounce = setTimeout(async () => {
    const ver = document.getElementById("version-select").value || "stable";
    const payload = {
      version: ver,
      options: Array.from(selectedOptionIds),
      path_overrides: pathOverrides,
      third_party_sources: thirdPartySources
    };

    try {
      const resp = await fetch(window.apiUrl("/api/nginx/preview"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      });
      const data = await resp.json();
      if (data.success && data.data) {
        document.getElementById("configure-preview").textContent = data.data.full_command;
        document.getElementById("preview-args-count").textContent = `${data.data.configure_args.length} ${t("badge_selected_count", "selected")}`;

        // Validation warnings
        const warnBox = document.getElementById("preview-warnings");
        if (data.data.validation_warnings && data.data.validation_warnings.length > 0) {
          warnBox.innerHTML = `<strong>⚠️ ${t("warn_adjust_title", "Parameter Adjustment Notice:")}</strong><br>${data.data.validation_warnings.join("<br>")}`;
          warnBox.classList.remove("hidden");
        } else {
          warnBox.classList.add("hidden");
        }

        // Mutual exclusion conflicts (informative soft notice)
        currentConflicts = data.data.conflicts || [];
        const conflictBox = document.getElementById("preview-conflicts");
        if (conflictBox) {
          if (currentConflicts.length > 0) {
            conflictBox.innerHTML = `<strong>⚠️ ${t("conflict_alert_title", "Option Conflict Notice")} (${currentConflicts.length}):</strong><br>${currentConflicts.map(c => `• ${c}`).join("<br>")}<br><span style="color:#94a3b8;font-size:11px;">${t("conflict_alert_sub", "(A resolution prompt will appear when you click build)")}</span>`;
            conflictBox.classList.remove("hidden");
          } else {
            conflictBox.classList.add("hidden");
          }
        }
      }
    } catch (err) {
      console.error("生成预览失败:", err);
    }
  }, 200);
}

// Start Build: Prompt user when conflicts are detected
async function startBuild() {
  if (currentConflicts && currentConflicts.length > 0) {
    showConflictModal(currentConflicts);
    return;
  }
  doSubmitBuild(false);
}

function showConflictModal(conflicts) {
  const modal = document.getElementById("conflict-modal");
  const listEl = document.getElementById("modal-conflict-list");
  if (!modal || !listEl) {
    if (confirm(t("modal_desc") + "\n\n" + conflicts.join("\n"))) {
      doSubmitBuild(true);
    }
    return;
  }

  listEl.innerHTML = conflicts.map(c => `
    <div class="conflict-item">
      <span class="conflict-bullet">•</span>
      <span>${c}</span>
    </div>
  `).join("");

  modal.classList.remove("hidden");

  const confirmBtn = document.getElementById("modal-confirm-btn");
  const cancelBtn = document.getElementById("modal-cancel-btn");
  const closeX = document.getElementById("modal-close-x");

  const closeModal = () => {
    modal.classList.add("hidden");
  };

  confirmBtn.onclick = () => {
    closeModal();
    doSubmitBuild(true);
  };

  cancelBtn.onclick = closeModal;
  closeX.onclick = closeModal;
}

async function doSubmitBuild(autoResolve) {
  const btn = document.getElementById("start-build-btn");
  btn.disabled = true;
  btn.innerHTML = `<span class="btn-icon">⏳</span><span class="btn-text">${t("btn_building", "Launching Build Environment...")}</span>`;

  const ver = document.getElementById("version-select").value || "stable";
  const payload = {
    version: ver,
    options: Array.from(selectedOptionIds),
    path_overrides: pathOverrides,
    third_party_sources: thirdPartySources,
    auto_resolve_conflicts: autoResolve
  };

  try {
    const resp = await fetch(window.apiUrl("/api/builds"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    });

    const data = await resp.json();
    if (!data.success) {
      alert(data.error || "Unknown error occurred");
      btn.disabled = false;
      btn.innerHTML = `<span class="btn-icon">⚡</span><span class="btn-text">${t("btn_start_build", "Build Nginx Now")}</span>`;
      return;
    }

    activeBuildId = data.build_id;
    watchBuild(activeBuildId);
  } catch (err) {
    alert("Network error: " + err.message);
    btn.disabled = false;
    btn.innerHTML = `<span class="btn-icon">⚡</span><span class="btn-text">${t("btn_start_build", "Build Nginx Now")}</span>`;
  }
}

// Watch Active Build
function watchBuild(buildId) {
  document.getElementById("terminal-body").textContent = "";
  document.getElementById("artifact-card").classList.add("hidden");
  document.getElementById("val-build-id").textContent = buildId;

  if (logEventSource) {
    logEventSource.close();
  }

  logEventSource = new EventSource(window.apiUrl(`/api/builds/${buildId}/logs?stream=true`));
  const term = document.getElementById("terminal-body");
  const autoscroll = document.getElementById("autoscroll-chk");

  logEventSource.onmessage = (e) => {
    term.textContent += e.data + "\n";
    if (autoscroll.checked) {
      term.scrollTop = term.scrollHeight;
    }
  };

  logEventSource.addEventListener("done", () => {
    logEventSource.close();
  });

  logEventSource.onerror = () => {};

  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(() => checkJobStatus(buildId), 1500);
  checkJobStatus(buildId);
}

async function checkJobStatus(buildId) {
  try {
    const resp = await fetch(window.apiUrl(`/api/builds/${buildId}`));
    const data = await resp.json();
    if (!data.success || !data.build) return;

    const b = data.build;
    updatePipelineUI(b);

    if (b.status === "completed" || b.status === "failed") {
      clearInterval(pollTimer);
      loadRecentBuilds();

      const btn = document.getElementById("start-build-btn");
      btn.disabled = false;
      btn.innerHTML = `<span class="btn-icon">⚡</span><span class="btn-text">${t("btn_start_build", "Build Nginx Now")}</span>`;

      if (b.status === "completed" && b.artifact) {
        showArtifactCard(b);
      }
    }
  } catch (err) {
    console.error("Query build status failed:", err);
  }
}

function updatePipelineUI(b) {
  const pill = document.getElementById("task-status-pill");
  pill.className = `status-badge status-${b.status}`;
  const statusLabels = {
    queued: t("step_queued", "Queued"),
    downloading: t("step_downloading", "Source Check"),
    configuring: t("step_configuring", "Configure"),
    building: t("step_building", "Compiling"),
    packaging: t("step_packaging", "Verification"),
    completed: t("step_completed", "Completed"),
    failed: t("step_failed", "Failed")
  };
  pill.textContent = statusLabels[b.status] || b.status;

  document.getElementById("val-step").textContent = b.current_step || "--";
  document.getElementById("val-duration").textContent = `${(b.duration_seconds || 0).toFixed(1)}s`;
  document.getElementById("val-platform").textContent = `${b.target_os} / ${b.target_arch}`;

  const steps = ["queued", "downloading", "configuring", "building", "packaging", "completed"];
  const currentIdx = steps.indexOf(b.status);

  document.querySelectorAll(".pipeline-step").forEach((el) => {
    const s = el.dataset.step;
    const idx = steps.indexOf(s);
    el.classList.remove("active", "completed");

    if (b.status === "completed") {
      el.classList.add("completed");
    } else if (idx < currentIdx) {
      el.classList.add("completed");
    } else if (idx === currentIdx) {
      el.classList.add("active");
    }
  });
}

function showArtifactCard(b) {
  const card = document.getElementById("artifact-card");
  card.classList.remove("hidden");

  document.getElementById("artifact-name").textContent = b.artifact.name;
  document.getElementById("artifact-sha").textContent = b.artifact.sha256;
  const sizeMb = (b.artifact.size / (1024 * 1024)).toFixed(2);
  document.getElementById("artifact-size").textContent = `${sizeMb} MB (${b.artifact.size} bytes)`;

  const dlLink = document.getElementById("artifact-download-link");
  const downloadUrl = b.artifact.download_url ? window.apiUrl(b.artifact.download_url) : "";
  dlLink.href = downloadUrl;
  dlLink.setAttribute("download", b.artifact.name);

  if (b.verify_result) {
    document.getElementById("verify-output").textContent = b.verify_result.raw_output || "Verified";
  }
}

// Load Recent Builds
async function loadRecentBuilds() {
  try {
    const resp = await fetch(window.apiUrl("/api/builds"));
    const data = await resp.json();
    if (!data.success || !data.builds) return;

    const tbody = document.getElementById("history-tbody");
    if (data.builds.length === 0) {
      tbody.innerHTML = `<tr><td colspan="6" class="text-center text-muted">${t("no_history", "No build records yet")}</td></tr>`;
      return;
    }

    tbody.innerHTML = "";
    data.builds.forEach(j => {
      const tr = document.createElement("tr");
      const timeStr = new Date(j.start_time).toLocaleString(currentLang === "zh" ? "zh-CN" : "en-US", { hour12: false });
      const statusBadge = `<span class="status-badge status-${j.status}">${t("step_" + j.status, j.status)}</span>`;
      const artifactUrl = j.artifact && j.artifact.download_url ? window.apiUrl(j.artifact.download_url) : "#";
      const actionHtml = j.status === "completed" && j.artifact ? 
        `<a href="${artifactUrl}" class="btn btn-xs btn-success" download>${t("action_download", "Download")}</a>` :
        `<button class="btn btn-xs btn-ghost" onclick="watchBuild('${j.build_id}')">${t("action_view", "View Logs")}</button>`;

      let extraDesc = "";
      if (j.third_party_sources) {
        const parts = [];
        if (j.third_party_sources.use_openssl_source) parts.push("OpenSSL");
        if (j.third_party_sources.use_pcre_source) parts.push("PCRE");
        if (j.third_party_sources.use_zlib_source) parts.push("zlib");
        if (parts.length > 0) extraDesc = ` <small style="color:var(--text-muted)">(${parts.join("+")})</small>`;
      }

      tr.innerHTML = `
        <td><code style="color:var(--info-color)">${j.build_id}</code></td>
        <td>Nginx ${j.nginx_version}${extraDesc}</td>
        <td>${statusBadge}</td>
        <td>${(j.duration_seconds || 0).toFixed(1)}s</td>
        <td>${timeStr}</td>
        <td>${actionHtml}</td>
      `;
      tbody.appendChild(tr);
    });
  } catch (err) {
    console.error("Load recent builds failed:", err);
  }
}

// ===================================================================
// System Environment & Dependency Management (Multi-Distro Support)
// ===================================================================

let currentSystemStatus = null;
let envLogSource = null;

async function loadSystemStatus() {
  try {
    const resp = await fetch(window.apiUrl("/api/system/status"));
    const res = await resp.json();
    if (!res.success || !res.data) return;

    currentSystemStatus = res.data;
    updateEnvUI(currentSystemStatus);
  } catch (err) {
    console.error("Failed to query system status:", err);
  }
}

function updateEnvUI(data) {
  const badgeDot = document.getElementById("env-badge-dot");
  const badgeText = document.getElementById("env-badge-text");
  const banner = document.getElementById("missing-deps-banner");
  const bannerDesc = document.getElementById("missing-deps-desc");

  if (data.all_installed) {
    if (badgeDot) badgeDot.className = "status-dot green";
    if (badgeText) badgeText.textContent = `${data.distro.distro_id} · ${t("env_status_ready", "Build Env Ready")}`;
    if (banner) banner.classList.add("hidden");
  } else {
    if (badgeDot) badgeDot.className = "status-dot amber";
    const msg = t("env_status_missing", "Missing {n} Dependencies (Click to Install)").replace("{n}", data.missing_count);
    if (badgeText) badgeText.textContent = `${data.distro.distro_id} · ${msg}`;
    
    if (banner) {
      banner.classList.remove("hidden");
      if (bannerDesc) {
        const missingNames = data.dependencies
          .filter(d => !d.installed && d.required)
          .map(d => d.package_name || d.name)
          .join(", ");
        bannerDesc.textContent = `${t("env_banner_desc", "Missing required build libraries/tools:")} ${missingNames}`;
      }
    }
  }

  // If modal is open, refresh its content
  const modal = document.getElementById("env-modal");
  if (modal && !modal.classList.contains("hidden")) {
    renderEnvModalContent(data);
  }
}

function openEnvModal() {
  const modal = document.getElementById("env-modal");
  if (!modal) return;
  modal.classList.remove("hidden");

  if (currentSystemStatus) {
    renderEnvModalContent(currentSystemStatus);
  } else {
    loadSystemStatus();
  }
}

function renderEnvModalContent(data) {
  document.getElementById("env-meta-distro").textContent = data.distro.distro_name || data.distro.distro_id;
  document.getElementById("env-meta-pkg").textContent = data.distro.package_manager;
  document.getElementById("env-meta-arch").textContent = `${data.distro.os} / ${data.distro.arch}`;

  const permEl = document.getElementById("env-meta-perm");
  if (data.distro.is_root) {
    permEl.className = "text-success";
    permEl.textContent = `✔ ${t("env_perm_root", "Root User (One-click install supported)")}`;
  } else if (data.distro.has_sudo) {
    permEl.className = "text-success";
    permEl.textContent = `✔ ${t("env_perm_sudo", "Passwordless Sudo (One-click install supported)")}`;
  } else {
    permEl.className = "text-warning";
    permEl.textContent = `⚠️ ${t("env_perm_unprivileged", "Unprivileged (Manual command required)")}`;
  }

  // Render Dependencies table
  const tbody = document.getElementById("env-deps-tbody");
  tbody.innerHTML = "";

  data.dependencies.forEach(d => {
    const tr = document.createElement("tr");
    const statusHtml = d.installed ? 
      `<span class="badge-installed">✔ ${t("status_installed", "Installed")}${d.version ? ` (${d.version})` : ""}</span>` :
      `<span class="badge-missing">⚠️ ${t("status_missing", "Missing")}</span>`;

    let catLabel = t("tab_core_libs", "Core Libraries");
    if (d.category === "toolchain") catLabel = t("tab_toolchain", "Build Toolchain");
    if (d.category === "opt_lib") catLabel = t("tab_opt_libs", "Optional Modules");

    tr.innerHTML = `
      <td><strong>${d.name}</strong><br><small style="color:var(--text-muted)">${d.description}</small></td>
      <td><span class="flag-tag">${catLabel}</span></td>
      <td><code>${d.package_name}</code></td>
      <td>${statusHtml}</td>
    `;
    tbody.appendChild(tr);
  });

  // Install command box
  const cmdWrap = document.getElementById("env-install-cmd-wrap");
  const cmdCode = document.getElementById("env-install-cmd");
  if (data.install_command) {
    cmdWrap.classList.remove("hidden");
    cmdCode.textContent = data.install_command;
  } else {
    cmdWrap.classList.add("hidden");
  }

  // Install button state
  const installBtn = document.getElementById("env-install-btn");
  if (data.all_installed) {
    installBtn.disabled = true;
    installBtn.innerHTML = `<span class="btn-icon">✔</span><span>${t("status_installed", "All Dependencies Ready")}</span>`;
    installBtn.style.opacity = "0.6";
  } else if (data.is_installing) {
    installBtn.disabled = true;
    installBtn.innerHTML = `<span class="btn-icon">⏳</span><span>${t("btn_installing_deps", "Installing Dependencies...")}</span>`;
  } else if (!data.distro.can_install) {
    installBtn.disabled = true;
    installBtn.innerHTML = `<span class="btn-icon">⚠️</span><span>${t("env_perm_unprivileged", "Manual Command Required")}</span>`;
    installBtn.style.opacity = "0.7";
  } else {
    installBtn.disabled = false;
    installBtn.innerHTML = `<span class="btn-icon">⚡</span><span>${t("btn_install_deps", "One-Click Install Dependencies")}</span>`;
    installBtn.style.opacity = "1";
  }
}

async function startInstallDeps() {
  const installBtn = document.getElementById("env-install-btn");
  installBtn.disabled = true;
  installBtn.innerHTML = `<span class="btn-icon">⏳</span><span>${t("btn_installing_deps", "Installing Dependencies...")}</span>`;

  const termWrap = document.getElementById("env-terminal-wrap");
  const termBody = document.getElementById("env-terminal-body");
  const statusPill = document.getElementById("env-install-status-pill");

  termWrap.classList.remove("hidden");
  termBody.textContent = `🚀 Starting system package installation on ${currentSystemStatus.distro.distro_name}...\n`;
  statusPill.textContent = t("btn_installing_deps", "Installing...");

  try {
    const resp = await fetch(window.apiUrl("/api/system/deps/install"), { method: "POST" });
    const res = await resp.json();
    if (!res.success) {
      termBody.textContent += `❌ ${res.error || "Failed to trigger dependency installation"}\n`;
      installBtn.disabled = false;
      return;
    }

    // Connect SSE log stream
    if (envLogSource) {
      envLogSource.close();
    }

    envLogSource = new EventSource(window.apiUrl("/api/system/deps/logs?stream=true"));
    envLogSource.onmessage = (e) => {
      termBody.textContent += e.data + "\n";
      termBody.scrollTop = termBody.scrollHeight;
    };

    envLogSource.addEventListener("done", () => {
      envLogSource.close();
      statusPill.textContent = "Finished";
      setTimeout(loadSystemStatus, 1000);
    });

    envLogSource.onerror = () => {
      setTimeout(loadSystemStatus, 2000);
    };

  } catch (err) {
    termBody.textContent += `❌ Network error: ${err.message}\n`;
    installBtn.disabled = false;
  }
}

// ===================================================================
// Parameter Presets & Scenario Templates Management
// ===================================================================

const fallbackPresetsList = [
  {
    id: "modern_web",
    name: "Standard Web & Reverse Proxy (Recommended)",
    display_name: "🌐 标准现代 Web 与反向代理 (推荐)",
    description: "生产主流推荐：启用 HTTPS (SSL/TLS)、HTTP/2、四层 Stream 转发、真实 IP 提取、预压缩静态文件直接分发 (Gzip Static)、状态监控及异步线程池加速。",
    options: ["http_ssl", "http_v2", "stream", "stream_ssl", "http_realip", "http_gzip_static", "http_stub_status", "pcre_jit", "threads"]
  },
  {
    id: "full_featured",
    name: "Full-Featured (All Official Modules)",
    display_name: "🚀 全功能官方模块合集",
    description: "启用官方绝大多数主流功能：包含 HTTP/2、HTTP/3 (QUIC)、四层全代理、SNI 预读、XSLT、图片剪裁、GeoIP、分片缓存、安全防盗链及动态模块兼容层。",
    options: ["http_ssl", "http_v2", "http_v3", "stream", "stream_ssl", "stream_ssl_preread", "stream_realip", "http_realip", "http_addition", "http_sub", "http_gunzip", "http_gzip_static", "http_auth_request", "http_secure_link", "http_slice", "http_stub_status", "threads", "file_aio", "pcre_jit", "compat"]
  },
  {
    id: "minimal",
    name: "Minimal & Tiny Server",
    display_name: "🪶 极简轻量服务器 (剥离不常用协议)",
    description: "剥离 FastCGI、uWSGI、SCGI、gRPC 等不需要的网关协议，编译极小体积的高性能轻量 Nginx，适合纯前端分发或轻量代理。",
    options: ["without_http_fastcgi", "without_http_uwsgi", "without_http_scgi", "without_http_grpc", "pcre_jit"]
  },
  {
    id: "media_streaming",
    name: "Media Streaming (HLS/MP4/FLV)",
    display_name: "🎬 音视频流媒体与大文件分发",
    description: "针对音视频点播与大文件分发优化：包含 MP4 关键帧拖拽寻道、FLV 伪流媒体、大文件分片 Slice 缓存、防盗链 Secure Link 以及高并发异步 I/O (File AIO)。",
    options: ["http_ssl", "http_v2", "http_flv", "http_mp4", "http_slice", "http_secure_link", "http_realip", "threads", "file_aio", "pcre_jit"]
  },
  {
    id: "l4_gateway",
    name: "L4 TCP/UDP Load Balancer",
    display_name: "🔀 四层 TCP/UDP 负载均衡网关",
    description: "专注于高性能四层流代理：支持 TCP/UDP 负载转发、SSL 终止与透传、SNI / ALPN 预读解析 (无需解密即可按域名路由) 及 Proxy Protocol 客户端真实 IP 传递。",
    options: ["stream", "stream_ssl", "stream_ssl_preread", "stream_realip", "threads", "pcre_jit", "http_ssl", "http_stub_status"]
  },
  {
    id: "security_hardened",
    name: "Security Hardened & Access Control",
    display_name: "🛡️ 安全访问控制与鉴权加固",
    description: "注重传输安全与权限拦截：启用 HTTPS/QUIC 加密通道、子请求外部统一鉴权 (Auth Request)、带时效与哈希签名的防盗链 (Secure Link) 以及真实客户端 IP 提取。",
    options: ["http_ssl", "http_v2", "http_v3", "http_realip", "http_auth_request", "http_secure_link", "http_stub_status", "pcre_jit"]
  },
  {
    id: "dynamic_compat",
    name: "Dynamic Modules Compatible",
    display_name: "🧩 动态模块二进制兼容 (compat)",
    description: "启用 --with-compat 保持二进制 ABI 兼容性，方便后续热加载外部第三方 .so 动态模块，并启用异步线程池与 PCRE JIT 即时编译加速。",
    options: ["compat", "http_ssl", "http_v2", "stream", "stream_ssl", "http_realip", "threads", "file_aio", "pcre_jit"]
  }
];

function renderPresetsUI() {
  const container = document.getElementById("presets-modal-grid");
  if (!container) return;

  const presets = (window.parameterPresets && window.parameterPresets.length > 0) ? window.parameterPresets : fallbackPresetsList;
  container.innerHTML = "";

  presets.forEach(p => {
    const card = document.createElement("div");
    card.className = "preset-card";
    
    // Check if currently all options are selected
    const isApplied = p.options.every(id => selectedOptionIds.has(id)) && selectedOptionIds.size === p.options.length;
    if (isApplied) card.classList.add("active");

    const localizedTitle = t("preset_" + p.id + "_title", p.display_name || p.name);
    const localizedDesc = t("preset_" + p.id + "_desc", p.description);
    const countText = t("preset_options_count", "{n} options included").replace("{n}", p.options.length);

    card.innerHTML = `
      <div class="preset-card-header">
        <h4 class="preset-card-title">${localizedTitle}</h4>
        <span class="badge-tag">${countText}</span>
      </div>
      <p class="preset-desc">${localizedDesc}</p>
      <div class="preset-tags">
        ${p.options.map(optId => `<code class="preset-tag-pill">${optId}</code>`).join("")}
      </div>
      <div class="preset-footer">
        <button class="btn btn-sm btn-primary" onclick="applyPresetTemplate('${p.id}')">
          <span class="btn-icon">✓</span>
          <span>${t("preset_btn_apply", "Apply Template")}</span>
        </button>
      </div>
    `;
    container.appendChild(card);
  });
}

function openPresetsModal() {
  renderPresetsUI();
  const modal = document.getElementById("presets-modal");
  if (modal) {
    modal.classList.remove("hidden");
  }
}

function closePresetsModal() {
  const modal = document.getElementById("presets-modal");
  if (modal) {
    modal.classList.add("hidden");
  }
}

function applyPresetTemplate(presetId, notify = true) {
  const presets = (window.parameterPresets && window.parameterPresets.length > 0) ? window.parameterPresets : fallbackPresetsList;
  const p = presets.find(item => item.id === presetId);
  if (!p) return;

  selectedOptionIds.clear();
  p.options.forEach(id => selectedOptionIds.add(id));

  renderTabs();
  renderOptionsGrid();
  renderPathsGrid();
  triggerPreview();

  closePresetsModal();
}

window.openPresetsModal = openPresetsModal;
window.closePresetsModal = closePresetsModal;
window.applyPresetTemplate = applyPresetTemplate;

// Wire Event Listeners for System Environment Management
document.addEventListener("DOMContentLoaded", () => {
  const btnPresetsModal = document.getElementById("btn-presets-menu");
  if (btnPresetsModal) {
    btnPresetsModal.addEventListener("click", openPresetsModal);
  }

  const presetsModalClose = document.getElementById("presets-modal-close");
  if (presetsModalClose) {
    presetsModalClose.addEventListener("click", closePresetsModal);
  }

  const presetsModalCancel = document.getElementById("presets-modal-cancel");
  if (presetsModalCancel) {
    presetsModalCancel.addEventListener("click", closePresetsModal);
  }
  const envBadge = document.getElementById("env-status-badge");
  if (envBadge) {
    envBadge.addEventListener("click", openEnvModal);
  }

  const bannerInstallBtn = document.getElementById("banner-install-btn");
  if (bannerInstallBtn) {
    bannerInstallBtn.addEventListener("click", openEnvModal);
  }

  const envModalClose = document.getElementById("env-modal-close");
  if (envModalClose) {
    envModalClose.addEventListener("click", () => {
      document.getElementById("env-modal").classList.add("hidden");
    });
  }

  const envRecheckBtn = document.getElementById("env-recheck-btn");
  if (envRecheckBtn) {
    envRecheckBtn.addEventListener("click", async () => {
      envRecheckBtn.disabled = true;
      await loadSystemStatus();
      envRecheckBtn.disabled = false;
    });
  }

  const envInstallBtn = document.getElementById("env-install-btn");
  if (envInstallBtn) {
    envInstallBtn.addEventListener("click", startInstallDeps);
  }

  const copyCmdBtn = document.getElementById("env-copy-cmd-btn");
  if (copyCmdBtn) {
    copyCmdBtn.addEventListener("click", () => {
      const code = document.getElementById("env-install-cmd").textContent;
      navigator.clipboard.writeText(code).then(() => {
        copyCmdBtn.textContent = t("copied", "Copied!");
        setTimeout(() => { copyCmdBtn.textContent = t("btn_copy", "Copy"); }, 2000);
      });
    });
  }
});
