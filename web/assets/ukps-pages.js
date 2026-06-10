const config = window.UKPS_PAGE;

const schemes = {
  chaps: {
    key: "chaps",
    name: "CHAPS",
    title: "CHAPS RTGS",
    baseUrl: "http://localhost:8420",
    paymentPath: "/v1/payments/chaps",
    limitsPath: "/v1/payments/chaps/limits",
    submitKind: "payment",
    sampleAmount: 250000,
  },
  fps: {
    key: "fps",
    name: "FPS",
    title: "Faster Payments Service",
    baseUrl: "http://localhost:8421",
    paymentPath: "/v1/payments/fps",
    limitsPath: "/v1/payments/fps/limits",
    submitKind: "payment",
    sampleAmount: 250,
  },
  bacs: {
    key: "bacs",
    name: "BACS",
    title: "BACS Standard 18",
    baseUrl: "http://localhost:8422",
    paymentPath: "/v1/payments/bacs/submit",
    limitsPath: "/v1/payments/bacs/limits",
    submitKind: "bacs",
    sampleAmount: 1250,
  },
};

const state = {
  participants: [],
  payments: [],
  cycles: [],
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

function setHTML(id, value) {
  document.querySelectorAll(`#${id}`).forEach((node) => {
    node.innerHTML = value;
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
  const labels = {
    ACCEPTED: "Przyjęte",
    ACTIVE: "Aktywny",
    AWAITING_SETTLEMENT: "Czeka na rozliczenie",
    DISABLED: "Wyłączony",
    OPEN: "Otwarty",
    PROCESSING: "Przetwarzany",
    RECALLED: "Wycofane",
    REJECTED: "Odrzucone",
    SETTLED: "Rozliczone",
    SUSPENDED: "Zawieszony",
  };
  return `<span class="pill ${clean.toLowerCase()}">${labels[clean] || clean}</span>`;
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

const infoLabels = {
  closing_time: "Zamknięcie",
  currency: "Waluta",
  daily_participant_limit: "Limit dzienny uczestnika",
  date: "Data",
  demo_session_minutes: "Sesja demo",
  available: "Dostępne środki",
  balance: "Saldo",
  bilateral: "Pozycje bilateralne",
  bic: "BIC",
  checks: "Kontrole",
  checks_passed: "Kontrole",
  cycle_date: "Data cyklu",
  cycle_id: "ID cyklu",
  cycle_status: "Status cyklu",
  earmarked: "Zarezerwowane",
  errors: "Błędy",
  is_closed: "Zamknięty",
  max_file_size: "Maks. rozmiar pliku",
  max_submission_value: "Maks. wartość zgłoszenia",
  max_transactions_per_file: "Maks. transakcji w pliku",
  name: "Nazwa",
  net_positions: "Pozycje netto",
  overdraft_limit: "Limit debetu",
  participant_type: "Typ uczestnika",
  reason_code: "Kod powodu",
  input_cutoff: "Koniec przyjmowania",
  interbank_cutoff: "Koniec międzybankowy",
  opening_time: "Otwarcie",
  remaining_intraday_liquidity: "Pozostała płynność",
  settlement_duration_minutes: "Czas rozliczenia",
  settlement_cycle: "Cykl rozliczenia",
  single_payment_limit: "Limit pojedynczej płatności",
  sort_code: "Sort code",
  status: "Status",
  timezone: "Strefa czasowa",
  total_available_liquidity: "Dostępna płynność",
  total_system_liquidity: "Płynność systemu",
  valid: "Wynik",
};

function formatInfoValue(key, value) {
  if (value == null || value === "" || value === "<nil>") return "-";
  if (Array.isArray(value)) {
    if (value.length === 0) return "-";
    if (value.every((item) => item == null || ["string", "number", "boolean"].includes(typeof item))) {
      return `<span class="tag-list">${value.map((item) => `<span>${escapeHTML(item)}</span>`).join("")}</span>`;
    }
    return `<span class="muted-value">${value.length} pozycji</span>`;
  }
  if (typeof value === "object") return `<span class="muted-value">Dane szczegółowe</span>`;
  if (typeof value === "boolean") return value ? "Tak" : "Nie";
  if (key === "valid") return value ? "Poprawna" : "Niepoprawna";
  if (key === "status") return statusPill(value);
  if (
    key.includes("amount") ||
    key.includes("available") ||
    key.includes("balance") ||
    key.includes("earmarked") ||
    key.includes("limit") ||
    key.includes("liquidity")
  ) {
    return money.format(Number(value));
  }
  if (key.includes("minutes")) return `${value} min`;
  return escapeHTML(value);
}

function renderInfoList(data, emptyText, preferredKeys = null) {
  if (!data || typeof data !== "object") {
    return `<p class="empty-state">${escapeHTML(emptyText)}</p>`;
  }
  const rawEntries = preferredKeys
    ? preferredKeys.filter((key) => Object.prototype.hasOwnProperty.call(data, key)).map((key) => [key, data[key]])
    : Object.entries(data);
  const entries = rawEntries.filter(([, value]) => value != null && value !== "" && value !== "<nil>");
  if (entries.length === 0) {
    return `<p class="empty-state">${escapeHTML(emptyText)}</p>`;
  }
  return `<dl>${entries
    .map(([key, value]) => `<div><dt>${infoLabels[key] || escapeHTML(key)}</dt><dd>${formatInfoValue(key, value)}</dd></div>`)
    .join("")}</dl>`;
}

function renderDetailSection(title, data, keys) {
  return `<section class="detail-section">
    <h3>${escapeHTML(title)}</h3>
    <div class="info-list compact">${renderInfoList(data, "Brak danych.", keys)}</div>
  </section>`;
}

function renderBankDetails(participant, position, limits) {
  return [
    renderDetailSection("Uczestnik", participant, ["bic", "name", "sort_code", "participant_type", "status", "is_closed"]),
    renderDetailSection("Pozycja", position, ["balance", "earmarked", "available"]),
    renderDetailSection("Limity", limits, ["currency", "single_payment_limit", "daily_participant_limit", "total_available_liquidity", "remaining_intraday_liquidity", "overdraft_limit"]),
  ].join("");
}

function renderResult(data, emptyText) {
  if (!data || typeof data !== "object") return escapeHTML(emptyText);
  return `<div class="info-list compact">${renderInfoList(data, emptyText)}</div>`;
}

async function api(path, options = {}) {
  const requestOptions = { ...options };
  const authToken = requestOptions.authToken ?? state.user?.apiKey;
  delete requestOptions.authToken;
  const headers = new Headers(requestOptions.headers || {});
  if (authToken && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${authToken}`);
  }
  requestOptions.headers = headers;

  const response = await fetch(scheme.baseUrl + path, requestOptions);
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

function inferBicFromApiKey(apiKey) {
  const match = String(apiKey || "").match(/^ak_([a-z0-9]{8,11})_dev$/i);
  return match ? match[1].toUpperCase() : "";
}

async function resolveBankIdentity(apiKey) {
  let position = null;
  try {
    position = await api("/v1/participants/positions", { authToken: apiKey });
  } catch {
    position = null;
  }
  const bic = String(position?.bic || inferBicFromApiKey(apiKey)).toUpperCase();
  if (!bic) {
    throw new Error("Nie można ustalić BIC dla tego API key. Backend potrzebuje endpointu /v1/participants/positions albo /v1/auth/me.");
  }
  return { bic, position };
}

async function centralTopUp(bic, amount) {
  const response = await fetch(`${scheme.baseUrl}/v1/liquidity/top-up`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ bic, amount }),
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
  const sortCode = item.receiver_sort_code || item.dest_sort_code || "";
  const bic = item.receiver_bic || item.creditor_bic || "";
  return sortCode ? `${bic} (${sortCode})` : bic;
}

function paymentAmount(item) {
  return Number(item.amount ?? item.total_value ?? item.value ?? item.gross_amount ?? 0);
}

function paymentStatus(item) {
  return item.status || item.iso_status || "-";
}

function bacsSettlementStatus(item) {
  if (scheme.key !== "bacs" || !item.cycle_id || paymentStatus(item) !== "ACCEPTED") {
    return paymentStatus(item);
  }
  const cycle = state.cycles.find((entry) => Number(entry.id) === Number(item.cycle_id));
  if (!cycle) return paymentStatus(item);
  if (cycle.status === "SETTLED") return "SETTLED";
  if (cycle.status === "AWAITING_SETTLEMENT") return "AWAITING_SETTLEMENT";
  if (cycle.status === "PROCESSING") return "PROCESSING";
  return paymentStatus(item);
}

function bankMatches(item, bic) {
  const sender = paymentSender(item);
  const receiver = item.receiver_bic || item.creditor_bic || "";
  return !sender || sender === bic || receiver === bic;
}

function normalizeSortCode(value) {
  const digits = String(value || "").replace(/\D/g, "");
  if (digits.length !== 6) return "";
  return `${digits.slice(0, 2)}-${digits.slice(2, 4)}-${digits.slice(4, 6)}`;
}

function findParticipantBySortCode(sortCode) {
  const normalized = normalizeSortCode(sortCode);
  if (!normalized) return null;
  return state.participants.find((participant) => normalizeSortCode(participant.sort_code) === normalized) || null;
}

function findParticipantByBicOrSortCode(value) {
  const clean = String(value || "").trim().toUpperCase();
  if (!clean) return null;
  return state.participants.find((participant) => participant.bic === clean) || findParticipantBySortCode(clean);
}

async function loadCoreData() {
  const [participants, payments, cycles] = await Promise.all([
    api("/v1/participants"),
    api(`${scheme.paymentPath}?limit=100`).catch(() => []),
    scheme.key === "bacs" ? api("/v1/payments/bacs/cycle").catch(() => []) : Promise.resolve([]),
  ]);
  state.participants = Array.isArray(participants) ? participants : [];
  state.payments = Array.isArray(payments) ? payments : [];
  state.cycles = Array.isArray(cycles) ? cycles : [];
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
      <td>${statusPill(bacsSettlementStatus(p))}</td>
    </tr>`)
    .join("");
}

function renderCycleOverview(cycles) {
  const target = $("cycleOverview");
  if (!target) return;
  if (!Array.isArray(cycles) || cycles.length === 0) {
    target.innerHTML = `<p class="empty-state">Brak danych cykli.</p>`;
    return;
  }

  const paymentCycleIds = new Set(state.payments.map((payment) => Number(payment.cycle_id)).filter(Boolean));
  const recentCycles = cycles.slice(0, 3);
  const recentIds = new Set(recentCycles.map((cycle) => Number(cycle.id)));
  const paymentCycles = cycles
    .filter((cycle) => paymentCycleIds.has(Number(cycle.id)) && !recentIds.has(Number(cycle.id)))
    .slice(0, 3);
  const renderCycleCard = (cycle) => `<article>
    <span>Cykl ${escapeHTML(cycle.id)}</span>
    ${statusPill(cycle.status)}
    <small>${escapeHTML(cycle.input_date)} -> ${escapeHTML(cycle.settlement_date)}</small>
  </article>`;

  target.innerHTML = `
    <p class="cycle-group-title">Najnowsze cykle</p>
    <div class="cycle-summary">
      ${recentCycles.map(renderCycleCard).join("")}
    </div>
    ${paymentCycles.length ? `
      <p class="cycle-group-title">Cykle ze zgłoszeniami</p>
      <div class="cycle-summary">
        ${paymentCycles.map(renderCycleCard).join("")}
      </div>
    ` : ""}
    <p class="cycle-help">Zamknięcie cyklu zawsze otwiera nowy cykl OPEN. Zgłoszenia z poprzedniego cyklu przechodzą dalej: PROCESSING -> AWAITING_SETTLEMENT -> SETTLED.</p>
  `;
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
    const settled = state.payments.filter((p) => ["SETTLED", "ACTC", "COMPLETED"].includes(bacsSettlementStatus(p))).length;
    const queued = state.payments.filter((p) => ["QUEUED", "PENDING", "RECEIVED", "PROCESSING", "AWAITING_SETTLEMENT", "PDNG"].includes(bacsSettlementStatus(p))).length;
    const rejected = state.payments.filter((p) => ["REJECTED", "RJCT", "RECALLED"].includes(bacsSettlementStatus(p))).length;
    setText("metricParticipants", String(state.participants.length));
    setText("metricSettled", String(settled));
    setText("metricQueued", String(queued));
    setText("metricRejected", String(rejected));
    setHTML("systemLimits", renderInfoList(limits, "Brak danych limitów."));
    setHTML("systemSchedule", renderInfoList(schedule, "Brak danych harmonogramu."));
    renderCycleOverview(state.cycles);
    setBusy(false, "Dane odświeżone");
  } catch (error) {
    setBusy(false, `Błąd: ${error.message}`);
  }
}

async function refreshBank() {
  const apiKey = state.user?.apiKey;
  if (!apiKey) return;
  setBusy(true, "Odświeżanie widoku banku...");
  try {
    const identity = await resolveBankIdentity(apiKey);
    const bic = identity.bic;
    state.user.bic = bic;
    sessionStorage.setItem(`ukps-${scheme.key}-bank`, JSON.stringify(state.user));
    await loadCoreData();
    const participant = state.participants.find((item) => item.bic === bic);
    const bankPayments = state.payments.filter((item) => bankMatches(item, bic));
    const position = identity.position;
    const limits = await api(`${scheme.limitsPath}?bic=${encodeURIComponent(bic)}`).catch(() => null);
    const balance = Number(participant?.balance || 0);
    const overdraft = Number(participant?.overdraft_limit || 0);
    setText("loggedBank", `${bic}${participant?.name ? ` - ${participant.name}` : ""}`);
    setText("metricBalance", money.format(balance));
    setText("metricAvailable", position?.available != null ? money.format(Number(position.available)) : money.format(balance + overdraft));
    setText("metricLimit", limits?.single_payment_limit != null ? money.format(Number(limits.single_payment_limit)) : money.format(overdraft));
    setText("metricBankPayments", String(bankPayments.length));
    setHTML("bankDetails", renderBankDetails(participant, position, limits));
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
  const senderBic = $("senderBic").value.trim().toUpperCase();
  const sender = state.participants.find((participant) => participant.bic === senderBic);
  const receiver = findParticipantByBicOrSortCode($("receiverIdentifier")?.value);
  if (!receiver) {
    throw new Error("Nie znaleziono odbiorcy o podanym BIC lub sort code.");
  }
  const receiverSortCode = normalizeSortCode(receiver.sort_code);

  return {
    msg_id: $("msgId").value.trim(),
    sender_bic: senderBic,
    receiver_bic: receiver.bic,
    sender_sort_code: normalizeSortCode(sender?.sort_code),
    receiver_sort_code: receiverSortCode,
    amount: Number($("amount").value),
  };
}

function standard18Sample() {
  const spaces = (n) => " ".repeat(n);
  const lpad = (value, length) => String(value).padStart(length, " ");
  const rpad = (value, length) => String(value).padEnd(length, " ");
  const rec1 = "1" + lpad("1", 7) + rpad("400000", 9) + rpad("01234567", 9) + spaces(29) + lpad("10000", 11) + lpad("1", 7) + "260526" + " ";
  const rec3 = "3" + lpad("1", 7) + rpad("400000", 9) + rpad("01234567", 9) + lpad("10000", 11) + rpad("ORIGINATOR", 15) + "1" + rpad("REF", 14) + rpad("XX1", 12) + " ";
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
  $("processCycle")?.addEventListener("click", () => jsonAction("Przetworzenie cyklu", () => api("/v1/payments/bacs/cycle/process", {
    method: "POST",
  }), refreshOperator));
  $("settleCycle")?.addEventListener("click", () => jsonAction("Rozliczenie cyklu", () => api("/v1/payments/bacs/cycle/settle", {
    method: "POST",
  }), refreshOperator));
}

function setupBankEvents() {
  $("loginForm")?.addEventListener("submit", async (event) => {
    event.preventDefault();
    setBusy(true, "Sprawdzanie API key...");
    try {
      const apiKey = $("loginApiKey").value.trim();
      if (!apiKey) throw new Error("Podaj API key.");
      const identity = await resolveBankIdentity(apiKey);
      const bic = identity.bic;
      state.user = { bic, apiKey };
      sessionStorage.setItem(`ukps-${scheme.key}-bank`, JSON.stringify(state.user));
      $("loginScreen").classList.add("hidden");
      $("appScreen").classList.remove("hidden");
      await refreshBank();
    } catch (error) {
      setBusy(false, `API key: ${error.message}`);
    }
  });
  $("logout")?.addEventListener("click", () => {
    sessionStorage.removeItem(`ukps-${scheme.key}-bank`);
    state.user = null;
    $("appScreen").classList.add("hidden");
    $("loginScreen").classList.remove("hidden");
    setBusy(false, "Usunięto API key");
  });
  $("refresh")?.addEventListener("click", refreshBank);
  $("paymentForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    if (scheme.submitKind === "bacs") {
      const content = $("standard18").value.trim();
      jsonAction("Przyjęcie pliku BACS do cyklu", () => api(`${scheme.paymentPath}?filename=bank-submission.txt`, {
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
      setHTML("validationResult", "BACS waliduje plik Standard 18 podczas wysyłki.");
      return;
    }
    jsonAction("Walidacja płatności", async () => {
      const result = await api(`${scheme.paymentPath}/validate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(buildPaymentPayload()),
      });
      setHTML("validationResult", renderResult(result, "Brak walidacji."));
    }, null);
  });
  $("loadNetting")?.addEventListener("click", async () => {
    const date = $("cycleDate").value || today;
    jsonAction("Pobranie raportu", async () => {
      const report = await api(`/v1/payments/bacs/reports/${date}/su`);
      setHTML("nettingResult", renderResult(report, "Brak raportu."));
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
    if (!state.user?.apiKey) {
      sessionStorage.removeItem(`ukps-${scheme.key}-bank`);
      state.user = null;
      return;
    }
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
