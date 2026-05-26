const config = window.UKPS_PAGE;

const schemes = {
  chaps: {
    key: "chaps",
    name: "CHAPS",
    title: "CHAPS RTGS",
    baseUrl: "http://localhost:8080",
    paymentPath: "/v1/payments/chaps",
    limitsPath: "/v1/payments/chaps/limits",
    submitKind: "payment",
    sampleAmount: 250000,
  },
  fps: {
    key: "fps",
    name: "FPS",
    title: "Faster Payments Service",
    baseUrl: "http://localhost:8081",
    paymentPath: "/v1/payments/fps",
    limitsPath: "/v1/payments/fps/limits",
    submitKind: "payment",
    sampleAmount: 250,
  },
  bacs: {
    key: "bacs",
    name: "BACS",
    title: "BACS Standard 18",
    baseUrl: "http://localhost:8082",
    paymentPath: "/v1/payments/bacs/submit",
    limitsPath: "/v1/payments/bacs/limits",
    submitKind: "bacs",
    sampleAmount: 1250,
  },
};

const state = {
  participants: [],
  payments: [],
  busy: false,
  user: null,
};

const scheme = schemes[config.scheme];
const money = new Intl.NumberFormat("en-GB", { style: "currency", currency: "GBP", maximumFractionDigits: 0 });
const today = new Date().toISOString().slice(0, 10);

function $(id) {
  return document.getElementById(id);
}

function setText(id, value) {
  document.querySelectorAll(`#${id}`).forEach((node) => {
    node.textContent = value;
  });
}

function setBusy(value, message) {
  state.busy = value;
  document.querySelectorAll("#statusDot").forEach((dot) => {
    dot.className = value ? "dot busy" : "dot";
  });
  setText("message", message || (value ? "Przetwarzanie..." : "Gotowe"));
  document.querySelectorAll("button").forEach((button) => {
    if (button.dataset.free !== "true") button.disabled = value;
  });
}

function statusPill(value) {
  const clean = String(value || "-");
  return `<span class="pill ${clean.toLowerCase()}">${clean}</span>`;
}

async function api(path, options = {}) {
  const response = await fetch(scheme.baseUrl + path, options);
  const text = await response.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  if (!response.ok) {
    const detail = typeof data === "string" ? data : data?.error || JSON.stringify(data);
    throw new Error(detail || `HTTP ${response.status}`);
  }
  return data;
}

async function centralTopUp(bic, amount) {
  const response = await fetch("http://localhost:8090/v1/central-bank/top-up", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ system: scheme.key, bic, amount, source: "BANK_OF_ENGLAND" }),
  });
  if (!response.ok) throw new Error(await response.text());
  return response.json();
}

function paymentId(item) {
  return item.msg_id || item.id || item.reference || "-";
}

function paymentSender(item) {
  return item.sender_bic || item.su_bic || item.debtor_bic || "";
}

function paymentReceiver(item) {
  return item.receiver_bic || item.creditor_bic || "";
}

function paymentAmount(item) {
  return Number(item.amount ?? item.total_value ?? item.value ?? item.gross_amount ?? 0);
}

function paymentStatus(item) {
  return item.status || item.iso_status || "-";
}

function bankMatches(item, bic) {
  const sender = paymentSender(item);
  const receiver = paymentReceiver(item);
  return !sender || sender === bic || receiver === bic;
}

async function loadCoreData() {
  const [participants, payments] = await Promise.all([
    api("/v1/participants"),
    api(`${scheme.paymentPath}?limit=100`).catch(() => []),
  ]);
  state.participants = Array.isArray(participants) ? participants : [];
  state.payments = Array.isArray(payments) ? payments : [];
}

function renderParticipantOptions(selectedBic) {
  document.querySelectorAll("[data-participant-options]").forEach((select) => {
    select.innerHTML = state.participants
      .map((participant) => {
        const selected = participant.bic === selectedBic ? "selected" : "";
        return `<option ${selected} value="${participant.bic}">${participant.bic} - ${participant.name}</option>`;
      })
      .join("");
  });
}

function renderParticipants() {
  const body = $("participantsBody");
  if (!body) return;
  body.innerHTML = state.participants
    .map((p) => {
      const balance = Number(p.balance || 0);
      const limit = Number(p.overdraft_limit || 0);
      const emergency = balance < -limit;
      return `<tr ${emergency ? 'class="notice bad"' : ""}>
        <td>${p.bic}</td>
        <td>${p.name || ""}</td>
        <td>${statusPill(p.status)}</td>
        <td>${money.format(balance)}</td>
        <td>${money.format(limit)}</td>
      </tr>`;
    })
    .join("");
}

function renderPayments(items = state.payments) {
  const body = $("paymentsBody");
  if (!body) return;
  body.innerHTML = items
    .map((p) => `<tr data-id="${paymentId(p)}">
      <td>${paymentId(p)}</td>
      <td>${paymentSender(p)}</td>
      <td>${paymentReceiver(p)}</td>
      <td>${money.format(paymentAmount(p))}</td>
      <td>${statusPill(paymentStatus(p))}</td>
    </tr>`)
    .join("");
}

async function refreshOperator() {
  setBusy(true, "Odświeżanie danych...");
  try {
    await loadCoreData();
    renderParticipants();
    renderPayments();
    renderParticipantOptions($("opsBic")?.value || state.participants[0]?.bic);
    const limits = await api(scheme.limitsPath).catch(() => null);
    const schedule = await api("/v1/system/schedule").catch(() => null);
    const settled = state.payments.filter((p) => ["SETTLED", "ACTC", "COMPLETED"].includes(paymentStatus(p))).length;
    const queued = state.payments.filter((p) => ["QUEUED", "PENDING", "RECEIVED", "PROCESSING", "PDNG"].includes(paymentStatus(p))).length;
    const rejected = state.payments.filter((p) => ["REJECTED", "RJCT", "RECALLED"].includes(paymentStatus(p))).length;
    setText("metricParticipants", String(state.participants.length));
    setText("metricSettled", String(settled));
    setText("metricQueued", String(queued));
    setText("metricRejected", String(rejected));
    setText("systemLimits", limits ? JSON.stringify(limits, null, 2) : "Brak danych limitów.");
    setText("systemSchedule", schedule ? JSON.stringify(schedule, null, 2) : "Brak danych harmonogramu.");
    setBusy(false, "Dane odświeżone");
  } catch (error) {
    setBusy(false, `Błąd: ${error.message}`);
  }
}

async function refreshBank() {
  const bic = state.user?.bic;
  if (!bic) return;
  setBusy(true, "Odświeżanie widoku banku...");
  try {
    await loadCoreData();
    const participant = state.participants.find((item) => item.bic === bic);
    const bankPayments = state.payments.filter((item) => bankMatches(item, bic));
    const position = await api(`/v1/participants/${bic}/positions`).catch(() => null);
    const limits = await api(`${scheme.limitsPath}?bic=${encodeURIComponent(bic)}`).catch(() => null);
    const balance = Number(participant?.balance || 0);
    const overdraft = Number(participant?.overdraft_limit || 0);
    setText("loggedBank", `${bic}${participant?.name ? ` - ${participant.name}` : ""}`);
    setText("metricBalance", money.format(balance));
    setText("metricAvailable", position?.available != null ? money.format(Number(position.available)) : money.format(balance + overdraft));
    setText("metricLimit", limits?.single_payment_limit != null ? money.format(Number(limits.single_payment_limit)) : money.format(overdraft));
    setText("metricBankPayments", String(bankPayments.length));
    setText("bankDetails", JSON.stringify({ participant, position, limits }, null, 2));
    renderPayments(bankPayments);
    renderParticipantOptions(bic);
    if ($("senderBic")) $("senderBic").value = bic;
    setBusy(false, "Widok banku odświeżony");
  } catch (error) {
    setBusy(false, `Błąd: ${error.message}`);
  }
}

async function jsonAction(label, action, after) {
  setBusy(true, `${label}...`);
  try {
    await action();
    setBusy(false, `${label}: wykonano`);
    if (after) await after();
  } catch (error) {
    setBusy(false, `${label}: ${error.message}`);
  }
}

function buildPaymentPayload() {
  return {
    msg_id: $("msgId").value.trim(),
    sender_bic: $("senderBic").value.trim().toUpperCase(),
    receiver_bic: $("receiverBic").value.trim().toUpperCase(),
    amount: Number($("amount").value),
  };
}

function standard18Sample() {
  const spaces = (n) => " ".repeat(n);
  const lpad = (value, length) => String(value).padStart(length, " ");
  const rpad = (value, length) => String(value).padEnd(length, " ");
  const rec1 = "1" + lpad("1", 7) + rpad("654321", 9) + rpad("01234567", 9) + spaces(29) + lpad("10000", 11) + lpad("1", 7) + "260526" + " ";
  const rec3 = "3" + lpad("1", 7) + rpad("654321", 9) + rpad("01234567", 9) + lpad("10000", 11) + rpad("ORIGINATOR", 15) + "1" + rpad("REF", 14) + rpad("XX1", 12) + " ";
  const rec5 = "5" + lpad("1", 7) + spaces(40) + lpad("1", 8) + spaces(24);
  const rec9 = "9" + lpad("1", 7) + spaces(12) + lpad("10000", 11) + lpad("1", 9) + lpad("10", 14) + spaces(26);
  return [
    rec1,
    rec3,
    rec5,
    rec9,
  ].join("\n");
}

function setupOperatorEvents() {
  $("refresh")?.addEventListener("click", refreshOperator);
  $("registerParticipant")?.addEventListener("submit", (event) => {
    event.preventDefault();
    jsonAction("Rejestracja uczestnika", () => api("/v1/participants/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        bic: $("newBic").value.trim().toUpperCase(),
        name: $("newName").value.trim(),
        balance: Number($("newBalance").value),
        su_code: $("newSuCode")?.value.trim(),
        is_service_user: $("newServiceUser")?.checked,
        is_destination_user: $("newDestUser")?.checked,
      }),
    }), refreshOperator);
  });
  $("setStatus")?.addEventListener("click", () => jsonAction("Zmiana statusu", () => api(`/v1/participants/${$("opsBic").value}/status`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ status: $("opsStatus").value, reason: $("opsReason").value }),
  }), refreshOperator));
  $("blockParticipant")?.addEventListener("click", () => jsonAction("Blokada uczestnika", () => api(`/v1/participants/${$("opsBic").value}/block`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ reason: $("opsReason").value }),
  }), refreshOperator));
  $("unblockParticipant")?.addEventListener("click", () => jsonAction("Odblokowanie uczestnika", () => api(`/v1/participants/${$("opsBic").value}/block`, {
    method: "DELETE",
  }), refreshOperator));
  $("topUp")?.addEventListener("click", () => jsonAction("Zasilenie płynności", () => centralTopUp($("opsBic").value, Number($("topUpAmount").value)), refreshOperator));
  $("updateLimit")?.addEventListener("click", () => jsonAction("Aktualizacja limitu", () => api(`${scheme.limitsPath}/${$("opsBic").value}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ overdraft_limit: Number($("overdraftLimit").value) }),
  }), refreshOperator));
  $("resolveGridlock")?.addEventListener("click", () => jsonAction("Rozwiązanie gridlock", () => api(`${scheme.paymentPath}/gridlock/resolve`, {
    method: "POST",
  }), refreshOperator));
  $("closeCycle")?.addEventListener("click", () => jsonAction("Zamknięcie cyklu", () => api("/v1/payments/bacs/cycle/close", {
    method: "POST",
  }), refreshOperator));
}

function setupBankEvents() {
  $("loginForm")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    setBusy(true, "Logowanie...");
    try {
      await loadCoreData();
      const bic = $("loginBic").value.trim().toUpperCase();
      const password = $("loginPassword").value;
      const exists = state.participants.some((item) => item.bic === bic);
      if (!exists) throw new Error("Nie znaleziono banku o podanym BIC.");
      if (password !== "bank123") throw new Error("Nieprawidłowe hasło demonstracyjne.");
      state.user = { bic };
      sessionStorage.setItem(`ukps-${scheme.key}-bank`, JSON.stringify(state.user));
      $("loginScreen").classList.add("hidden");
      $("appScreen").classList.remove("hidden");
      await refreshBank();
    } catch (error) {
      setBusy(false, `Logowanie: ${error.message}`);
    }
  });
  $("logout")?.addEventListener("click", () => {
    sessionStorage.removeItem(`ukps-${scheme.key}-bank`);
    state.user = null;
    $("appScreen").classList.add("hidden");
    $("loginScreen").classList.remove("hidden");
    setBusy(false, "Wylogowano");
  });
  $("refresh")?.addEventListener("click", refreshBank);
  $("paymentForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    if (scheme.submitKind === "bacs") {
      const content = $("standard18").value.trim();
      const su = state.user.bic;
      jsonAction("Wysłanie pliku BACS", () => api(`${scheme.paymentPath}?su_bic=${encodeURIComponent(su)}&filename=bank-submission.txt`, {
        method: "POST",
        headers: { "Content-Type": "text/plain" },
        body: content,
      }), refreshBank);
      return;
    }
    jsonAction("Wysłanie płatności", () => api(scheme.paymentPath, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(buildPaymentPayload()),
    }), refreshBank);
  });
  $("validatePayment")?.addEventListener("click", () => {
    if (scheme.submitKind === "bacs") {
      setText("validationResult", "BACS waliduje plik Standard 18 podczas wysyłki.");
      return;
    }
    jsonAction("Walidacja płatności", async () => {
      const result = await api(`${scheme.paymentPath}/validate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(buildPaymentPayload()),
      });
      setText("validationResult", JSON.stringify(result, null, 2));
    }, null);
  });
  $("loadNetting")?.addEventListener("click", async () => {
    const date = $("cycleDate").value || today;
    jsonAction("Pobranie raportu", async () => {
      const report = await api(`/v1/payments/bacs/reports/${date}/netting/${state.user.bic}`);
      setText("nettingResult", JSON.stringify(report, null, 2));
    }, null);
  });
}

function initCommonLabels() {
  document.title = `${scheme.name} - ${config.role === "bank" ? "Bank" : "Operator"}`;
  document.querySelectorAll("[data-scheme-name]").forEach((node) => {
    node.textContent = scheme.name;
  });
  document.querySelectorAll("[data-scheme-title]").forEach((node) => {
    node.textContent = scheme.title;
  });
  if ($("msgId")) $("msgId").value = `${scheme.name}-GUI-${Date.now().toString().slice(-8)}`;
  if ($("amount")) $("amount").value = scheme.sampleAmount;
  if ($("cycleDate")) $("cycleDate").value = today;
  if ($("standard18")) $("standard18").value = standard18Sample();
}

async function initBankSession() {
  const saved = sessionStorage.getItem(`ukps-${scheme.key}-bank`);
  if (!saved) return;
  try {
    state.user = JSON.parse(saved);
    $("loginScreen").classList.add("hidden");
    $("appScreen").classList.remove("hidden");
    await refreshBank();
  } catch {
    sessionStorage.removeItem(`ukps-${scheme.key}-bank`);
  }
}

async function init() {
  initCommonLabels();
  if (config.role === "operator") {
    setupOperatorEvents();
    await refreshOperator();
  } else {
    setupBankEvents();
    await initBankSession();
  }
}

init().catch((error) => setBusy(false, `Start: ${error.message}`));
