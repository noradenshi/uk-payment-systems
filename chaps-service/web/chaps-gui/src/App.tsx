import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  amendPayment,
  authorizePayment,
  blockParticipant,
  cancelPayment,
  createPayment,
  getBlockDetails,
  getLimits,
  getPosition,
  getSchedule,
  listParticipants,
  listPayments,
  registerParticipant,
  topUpLiquidity,
  unblockParticipant,
  updateParticipantStatus,
  validatePayment,
} from "./api";
import type { Limits, Participant, Payment, Position, Schedule, ValidationResult } from "./types";

const money = new Intl.NumberFormat("en-GB", { style: "currency", currency: "GBP", maximumFractionDigits: 0 });

const paymentStatusLabels: Record<Payment["status"], string> = {
  PENDING: "Oczekująca",
  QUEUED: "W kolejce",
  SETTLED: "Rozliczona",
  REJECTED: "Odrzucona",
};

const participantStatusLabels: Record<Participant["status"], string> = {
  ACTIVE: "Aktywny",
  SUSPENDED: "Zawieszony",
  DISABLED: "Wyłączony",
};

function nextMsgId() {
  return `CHAPS-GUI-${Date.now().toString().slice(-8)}`;
}

export default function App() {
  const [participants, setParticipants] = useState<Participant[]>([]);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [limits, setLimits] = useState<Limits | null>(null);
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [position, setPosition] = useState<Position | null>(null);
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [message, setMessage] = useState("Gotowe");
  const [busy, setBusy] = useState(false);

  const [paymentForm, setPaymentForm] = useState({
    msg_id: nextMsgId(),
    sender_bic: "BARCGB2L",
    receiver_bic: "HSBCGB44",
    amount: 250000,
  });
  const [participantForm, setParticipantForm] = useState({ bic: "", name: "", balance: 100000 });
  const [opsForm, setOpsForm] = useState({ bic: "BARCGB2L", amount: 50000, status: "ACTIVE" as Participant["status"], reason: "FRAUD_SUSPECTED" });
  const [paymentOps, setPaymentOps] = useState({ msgId: "", remittanceInfo: "Zaktualizowany opis przelewu" });

  const totals = useMemo(() => {
    const settled = payments.filter((payment) => payment.status === "SETTLED").length;
    const queued = payments.filter((payment) => payment.status === "QUEUED" || payment.status === "PENDING").length;
    const rejected = payments.filter((payment) => payment.status === "REJECTED").length;
    return { settled, queued, rejected };
  }, [payments]);

  async function refresh() {
    const [participantData, paymentData, limitData, scheduleData] = await Promise.all([
      listParticipants(),
      listPayments(),
      getLimits(),
      getSchedule(),
    ]);
    setParticipants(participantData);
    setPayments(paymentData);
    setLimits(limitData);
    setSchedule(scheduleData);
    if (!paymentOps.msgId && paymentData[0]) {
      setPaymentOps((current) => ({ ...current, msgId: paymentData[0].msg_id }));
    }
  }

  useEffect(() => {
    refresh().catch((error) => setMessage(error.message));
  }, []);

  async function run(label: string, action: () => Promise<unknown>, shouldRefresh = true) {
    setBusy(true);
    setMessage(`${label}...`);
    try {
      const result = await action();
      setMessage(`${label}: wykonano`);
      if (shouldRefresh) {
        await refresh();
      }
      return result;
    } catch (error) {
      setMessage(`${label}: ${(error as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  function submitPayment(event: FormEvent) {
    event.preventDefault();
    run("Utworzenie płatności CHAPS", async () => {
      const result = await createPayment(paymentForm);
      setPaymentOps((current) => ({ ...current, msgId: paymentForm.msg_id }));
      setPaymentForm((current) => ({ ...current, msg_id: nextMsgId() }));
      return result;
    });
  }

  function submitValidation() {
    run("Walidacja płatności", async () => {
      const result = await validatePayment(paymentForm);
      setValidation(result);
      return result;
    }, false);
  }

  function submitParticipant(event: FormEvent) {
    event.preventDefault();
    run("Rejestracja uczestnika", () => registerParticipant(participantForm));
  }

  return (
    <main className="shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Operacje CHAPS RTGS</p>
          <h1>Panel operatora CHAPS</h1>
        </div>
        <div className="status-strip">
          <span className={busy ? "dot busy" : "dot"} />
          <strong>{message}</strong>
        </div>
      </section>

      <section className="metrics-grid">
        <article className="metric-card"><span>Rozliczone</span><strong>{totals.settled}</strong></article>
        <article className="metric-card"><span>W kolejce / oczekujące</span><strong>{totals.queued}</strong></article>
        <article className="metric-card"><span>Odrzucone</span><strong>{totals.rejected}</strong></article>
        <article className="metric-card"><span>Płynność</span><strong>{limits ? money.format(limits.total_available_liquidity) : "-"}</strong></article>
      </section>

      <section className="work-grid">
        <form className="panel" onSubmit={submitPayment}>
          <div className="panel-head">
            <div>
              <p className="section-label">POST /v1/payments/chaps</p>
              <h2>Nowa płatność</h2>
            </div>
            <button type="button" className="secondary-button" onClick={submitValidation}>Waliduj</button>
          </div>
          <div className="form-grid">
            <label>Identyfikator wiadomości<input value={paymentForm.msg_id} onChange={(e) => setPaymentForm({ ...paymentForm, msg_id: e.target.value })} /></label>
            <label>BIC nadawcy<input value={paymentForm.sender_bic} onChange={(e) => setPaymentForm({ ...paymentForm, sender_bic: e.target.value.toUpperCase() })} /></label>
            <label>BIC odbiorcy<input value={paymentForm.receiver_bic} onChange={(e) => setPaymentForm({ ...paymentForm, receiver_bic: e.target.value.toUpperCase() })} /></label>
            <label>Kwota<input type="number" value={paymentForm.amount} onChange={(e) => setPaymentForm({ ...paymentForm, amount: Number(e.target.value) })} /></label>
          </div>
          {validation && (
            <div className={validation.valid ? "notice ok" : "notice bad"}>
              {validation.valid ? "Walidacja zakończona powodzeniem" : validation.errors.join(", ")}. Dostępne środki: {money.format(validation.available)}
            </div>
          )}
          <button className="primary-button" disabled={busy}>Wyślij płatność</button>
        </form>

        <form className="panel" onSubmit={submitParticipant}>
          <div className="panel-head">
            <div>
              <p className="section-label">POST /v1/participants/register</p>
              <h2>Rejestracja uczestnika</h2>
            </div>
          </div>
          <div className="form-grid">
            <label>BIC<input value={participantForm.bic} onChange={(e) => setParticipantForm({ ...participantForm, bic: e.target.value.toUpperCase() })} /></label>
            <label>Nazwa<input value={participantForm.name} onChange={(e) => setParticipantForm({ ...participantForm, name: e.target.value })} /></label>
            <label>Saldo początkowe<input type="number" value={participantForm.balance} onChange={(e) => setParticipantForm({ ...participantForm, balance: Number(e.target.value) })} /></label>
          </div>
          <button className="primary-button" disabled={busy}>Zarejestruj bank</button>
        </form>
      </section>

      <section className="work-grid">
        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Kontrola uczestnika</p>
              <h2>Status, blokada i płynność</h2>
            </div>
          </div>
          <div className="form-grid">
            <label>BIC<input value={opsForm.bic} onChange={(e) => setOpsForm({ ...opsForm, bic: e.target.value.toUpperCase() })} /></label>
            <label>Status<select value={opsForm.status} onChange={(e) => setOpsForm({ ...opsForm, status: e.target.value as Participant["status"] })}><option value="ACTIVE">Aktywny</option><option value="SUSPENDED">Zawieszony</option><option value="DISABLED">Wyłączony</option></select></label>
            <label>Powód<input value={opsForm.reason} onChange={(e) => setOpsForm({ ...opsForm, reason: e.target.value })} /></label>
            <label>Zasilenie<input type="number" value={opsForm.amount} onChange={(e) => setOpsForm({ ...opsForm, amount: Number(e.target.value) })} /></label>
          </div>
          <div className="button-row">
            <button className="secondary-button" onClick={() => run("Zmiana statusu", () => updateParticipantStatus(opsForm.bic, opsForm.status, opsForm.reason))}>Ustaw status</button>
            <button className="secondary-button" onClick={() => run("Blokada uczestnika", () => blockParticipant(opsForm.bic, opsForm.reason))}>Zablokuj</button>
            <button className="secondary-button" onClick={() => run("Odblokowanie uczestnika", () => unblockParticipant(opsForm.bic))}>Odblokuj</button>
            <button className="secondary-button" onClick={() => run("Zasilenie płynności", () => topUpLiquidity({ bic: opsForm.bic, amount: opsForm.amount }))}>Zasil</button>
            <button className="secondary-button" onClick={() => run("Pobranie pozycji", async () => setPosition(await getPosition(opsForm.bic)), false)}>Pozycja</button>
            <button className="secondary-button" onClick={() => run("Szczegóły blokady", () => getBlockDetails(opsForm.bic), false)}>Szczegóły blokady</button>
          </div>
          {position && <div className="notice">Dostępne środki dla {position.bic}: {money.format(position.available)}</div>}
        </article>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Kontrola płatności</p>
              <h2>Autoryzacja, korekta, anulowanie</h2>
            </div>
          </div>
          <div className="form-grid">
            <label>Identyfikator wiadomości<input value={paymentOps.msgId} onChange={(e) => setPaymentOps({ ...paymentOps, msgId: e.target.value })} /></label>
            <label>Opis przelewu<input value={paymentOps.remittanceInfo} onChange={(e) => setPaymentOps({ ...paymentOps, remittanceInfo: e.target.value })} /></label>
          </div>
          <div className="button-row">
            <button className="secondary-button" onClick={() => run("Autoryzacja płatności", () => authorizePayment(paymentOps.msgId))}>Autoryzuj</button>
            <button className="secondary-button" onClick={() => run("Korekta płatności", () => amendPayment(paymentOps.msgId, paymentOps.remittanceInfo))}>Koryguj</button>
            <button className="danger-button" onClick={() => run("Anulowanie płatności", () => cancelPayment(paymentOps.msgId))}>Anuluj</button>
          </div>
          {schedule && <div className="notice">Harmonogram {schedule.date}: {schedule.opening_time}-{schedule.interbank_cutoff} {schedule.timezone}</div>}
        </article>
      </section>

      <section className="table-grid">
        <article className="panel">
          <div className="panel-head"><div><p className="section-label">GET /v1/participants</p><h2>Uczestnicy</h2></div></div>
          <table>
            <thead><tr><th>BIC</th><th>Nazwa</th><th>Status</th><th>Saldo</th></tr></thead>
            <tbody>
              {participants.map((participant) => (
                <tr key={participant.bic}>
                  <td>{participant.bic}</td>
                  <td>{participant.name}</td>
                  <td><span className={`pill ${participant.status.toLowerCase()}`}>{participantStatusLabels[participant.status]}</span></td>
                  <td>{money.format(participant.balance)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </article>

        <article className="panel">
          <div className="panel-head"><div><p className="section-label">GET /v1/payments/chaps</p><h2>Płatności</h2></div></div>
          <table>
            <thead><tr><th>Id wiadomości</th><th>Trasa</th><th>Kwota</th><th>Status</th></tr></thead>
            <tbody>
              {payments.map((payment) => (
                <tr key={payment.msg_id} onClick={() => setPaymentOps((current) => ({ ...current, msgId: payment.msg_id }))}>
                  <td>{payment.msg_id}</td>
                  <td>{payment.sender_bic} &gt; {payment.receiver_bic}</td>
                  <td>{money.format(payment.amount)}</td>
                  <td><span className={`pill ${payment.status.toLowerCase()}`}>{paymentStatusLabels[payment.status]}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </article>
      </section>
    </main>
  );
}
