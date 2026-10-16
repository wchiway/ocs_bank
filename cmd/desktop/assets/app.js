(() => {
  "use strict";

  const $ = (id) => document.getElementById(id);
  const form = $("settings-form");
  const numericFields = ["port", "timeout_seconds", "max_concurrent", "max_pending", "max_retries", "max_tokens", "requests_per_minute", "cache_ttl_hours"];
  const protocolNames = { chat: "Chat Completions", chat_completions: "Chat Completions", message: "Messages", messages: "Messages", response: "Responses", responses: "Responses" };
  let settings;
  let status = { state: "stopped", counters: {} };
  let busy = false;
  let polling = false;
  let pollFailures = 0;
  let noticeTimer;

  function call(method, ...args) {
    if (!window.mygo) return Promise.reject(new Error("桌面连接尚未就绪，请关闭后重新打开应用。"));
    return window.mygo.call(`Desktop.${method}`, ...args);
  }

  function showError(error) {
    $("notice-text").textContent = error instanceof Error ? error.message : String(error);
    $("notice").hidden = false;
  }

  function toast(message) {
    clearTimeout(noticeTimer);
    $("toast").querySelector("span").textContent = message;
    $("toast").hidden = false;
    noticeTimer = setTimeout(() => { $("toast").hidden = true; }, 2800);
  }

  function navigate(view) {
    document.querySelectorAll(".view").forEach((section) => { section.hidden = section.id !== `view-${view}`; });
    document.querySelectorAll(".nav-item").forEach((button) => {
      const active = button.dataset.view === view;
      button.classList.toggle("active", active);
      if (active) button.setAttribute("aria-current", "page");
      else button.removeAttribute("aria-current");
    });
    $("page-label").textContent = { service: "题库服务", settings: "模型设置", ocs: "OCS 接入" }[view];
    if (view === "ocs") refreshExport();
  }

  function fillSettings(next) {
    settings = next;
    for (const [name, value] of Object.entries(next)) {
      const input = form.elements.namedItem(name);
      if (input && name !== "api_key") input.value = value ?? "";
    }
    const protocol = { chat_completions: "chat", messages: "message", responses: "response" }[next.protocol] || next.protocol;
    form.elements.namedItem("protocol").value = protocol;
    $("api-key").value = "";
    $("api-key").required = !next.has_api_key;
    $("api-key").placeholder = next.has_api_key ? "已保存；留空保留原密钥" : "填写 API Key";
    $("key-help").textContent = next.has_api_key ? "已安全保存。无需重复填写；输入新密钥可替换。" : "密钥单独保存在本机，保存后不回显。";
    $("current-model").textContent = next.model || "尚未配置";
    $("current-protocol").textContent = next.configured ? protocolNames[next.protocol] || next.protocol : "先填写模型与 API Key，即可开始使用。";
    $("current-concurrency").textContent = `${next.max_concurrent} 题`;
    $("current-rpm").textContent = `${next.requests_per_minute} 次 / 分钟`;
    $("service-url").textContent = `http://127.0.0.1:${next.port}/ocs/query`;
    $("ocs-url").textContent = $("service-url").textContent;
    renderStatus();
  }

  function renderStatus() {
    const running = status.state === "running";
    const stopping = status.state === "stopping";
    const starting = status.state === "starting";
    const transitioning = starting || stopping;
    const configured = Boolean(settings?.configured);
    const label = busy ? "正在处理" : running ? "正在运行" : starting ? "正在启动" : stopping ? "正在停止" : configured ? "服务已停止" : "等待配置";
    $("top-status-text").textContent = label;
    $("top-status").classList.toggle("running", running);
    document.querySelector(".service-panel").classList.toggle("running", running);
    $("service-state").textContent = label;
    $("service-description").textContent = running ? "题库服务已就绪，可以在 OCS 中开始搜题。" : stopping ? "正在结束进行中的请求，请稍候。" : configured ? "模型配置已保存，启动后即可接收查询。" : "先连接模型，再启动你的本地题库。";
    $("toggle-service").textContent = busy ? "正在处理…" : running ? "停止服务" : stopping ? "正在停止…" : configured ? "启动服务" : "配置模型";
    $("toggle-service").disabled = busy || transitioning || !settings;
    $("settings-fields").disabled = busy || running || transitioning || !settings;
    $("settings-locked").hidden = !running && !transitioning;
    for (const id of ["save-settings", "save-start"]) $(id).disabled = busy || running || transitioning || !settings;
    $("stop-to-edit").disabled = busy || transitioning;
    for (const id of ["copy-url", "copy-ocs", "save-ocs"]) $(id).disabled = busy || !configured;
    const counters = status.counters || {};
    const count = (name) => Number(counters[name] || 0);
    const format = (n) => n.toLocaleString("zh-CN");
    $("metric-queries").textContent = format(count("queries_total"));
    $("metric-hits").textContent = format(count("query_layer_reviewed") + count("query_layer_cache"));
    $("metric-upstream").textContent = format(count("upstream_calls_total"));
    $("metric-pending").textContent = `${format(count("active_generations"))} / ${format(Math.max(0, count("pending_generations") - count("active_generations")))}`;
    const started = typeof status.started_at === "number" ? status.started_at * 1000 : Date.parse(status.started_at);
    if (running && Number.isFinite(started) && started > 0) {
      const seconds = Math.max(0, Math.floor((Date.now() - started) / 1000));
      $("uptime").textContent = seconds < 60 ? "刚刚启动" : `已运行 ${Math.floor(seconds / 60)} 分钟`;
    } else $("uptime").textContent = running ? "已启动" : "未在运行";
  }

  async function perform(action) {
    if (busy) return;
    busy = true;
    $("notice").hidden = true;
    renderStatus();
    try { await action(); }
    catch (error) { showError(error); }
    finally {
      try { status = await call("Status"); } catch { /* Keep the last known state after a bridge failure. */ }
      busy = false;
      renderStatus();
    }
  }

  async function refreshExport() {
    if (!settings?.configured) return;
    try {
      const output = await call("ExportOCS");
      $("ocs-url").textContent = output.url;
      $("ocs-json").textContent = output.json;
    } catch (error) { showError(error); }
  }

  document.querySelectorAll("[data-view]").forEach((button) => button.addEventListener("click", () => navigate(button.dataset.view)));
  $("dismiss-notice").addEventListener("click", () => { $("notice").hidden = true; });
  $("toggle-service").addEventListener("click", () => {
    if (!settings?.configured) { navigate("settings"); form.elements.namedItem("model").focus(); return; }
    perform(async () => {
      status = await call(status.state === "running" ? "Stop" : "Start");
      toast(status.state === "running" ? "题库服务已启动" : "题库服务已停止");
    });
  });
  $("stop-to-edit").addEventListener("click", () => perform(async () => { status = await call("Stop"); toast("已停止，可以修改设置"); }));

  form.addEventListener("submit", (event) => {
    event.preventDefault();
    if (busy || status.state !== "stopped") return;
    const values = Object.fromEntries(new FormData(form));
    for (const name of numericFields) values[name] = Number(values[name]);
    if (values.max_pending < values.max_concurrent) { showError(new Error("最大等待调用者不能小于同时生成的题目数。")); return; }
    if (!["response", "responses"].includes(values.protocol) && values.reasoning_effort) { showError(new Error("推理强度仅适用于 Responses 协议，请改为跟随上游默认。")); return; }
    const start = event.submitter?.value === "start";
    perform(async () => {
      fillSettings(await call("SaveSettings", values));
      if (start) {
        status = await call("Start");
        navigate("service");
        toast("设置已保存，题库服务已启动");
      } else toast("设置已保存");
    });
  });

  for (const [id, method, success] of [["copy-url", "CopyURL", "查询地址已复制"], ["copy-ocs", "CopyOCS", "完整配置已复制，可粘贴到 OCS"], ["open-folder", "OpenDataFolder", ""]]) {
    $(id).addEventListener("click", () => perform(async () => { await call(method); if (success) toast(success); }));
  }
  $("save-ocs").addEventListener("click", () => perform(async () => { if (await call("SaveOCS")) toast("OCS 配置已保存"); }));
  $("quit").addEventListener("click", () => call("Quit").catch(showError));

  async function poll() {
    if (busy || polling || document.hidden || !settings) return;
    polling = true;
    try {
      const next = await call("Status");
      if (!busy) {
        const previousError = status.last_error;
        status = next;
        renderStatus();
        if (next.last_error && next.last_error !== previousError) showError(new Error(next.last_error));
      }
      pollFailures = 0;
    } catch (error) { if (++pollFailures === 3) showError(error); }
    finally { polling = false; }
  }

  async function initialize() {
    try {
      const [saved, current] = await Promise.all([call("Settings"), call("Status")]);
      status = current;
      fillSettings(saved);
      if (!saved.configured) navigate("settings");
      else if (status.state === "stopped") await perform(async () => { status = await call("Start"); });
      if (status.last_error) showError(new Error(status.last_error));
      setInterval(poll, 2000);
    } catch (error) {
      $("service-state").textContent = "无法载入配置";
      $("service-description").textContent = "检查数据目录是否可读，然后重新打开应用。";
      $("top-status-text").textContent = "需要处理";
      showError(error);
    }
  }
  initialize();
})();
