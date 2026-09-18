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
  document.getElementById("btn-select-all").addEventListener("click", () => {
    recommendedPresets.forEach(id => selectedOptionIds.add(id));
    renderOptionsGrid();
    triggerPreview();
  });

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
    const resp = await fetch("/api/nginx/versions");
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
    const resp = await fetch("/api/nginx/options");
    const data = await resp.json();
    if (!data.success) return;

    allOptions = data.options || [];
    optionCategories = data.categories || {};
    pathDefaults = data.path_defaults || {};
    depLibraries = data.dep_libraries || {};

    // Populate initial recommended modules
    recommendedPresets.forEach(id => selectedOptionIds.add(id));

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
      const resp = await fetch("/api/nginx/preview", {
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
    const resp = await fetch("/api/builds", {
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

  logEventSource = new EventSource(`/api/builds/${buildId}/logs?stream=true`);
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
    const resp = await fetch(`/api/builds/${buildId}`);
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
  dlLink.href = b.artifact.download_url;
  dlLink.setAttribute("download", b.artifact.name);

  if (b.verify_result) {
    document.getElementById("verify-output").textContent = b.verify_result.raw_output || "Verified";
  }
}

// Load Recent Builds
async function loadRecentBuilds() {
  try {
    const resp = await fetch("/api/builds");
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
      const actionHtml = j.status === "completed" && j.artifact ? 
        `<a href="${j.artifact.download_url}" class="btn btn-xs btn-success" download>${t("action_download", "Download")}</a>` :
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
