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

type ViewMode = "bank" | "operator";

const serviceName = "CHAPS RTGS";
const money = new Intl.NumberFormat("en-GB", { style: "currency", currency: "GBP", maximumFractionDigits: 0 });

const paymentStatusLabels: Record<Payment["status"], string> = {
  PENDING: "Oczekujaca",
  QUEUED: "W kolejce",
  SETTLED: "Rozliczona",
  REJECTED: "Odrzucona",
};

const participantStatusLabels: Record<Participant["status"], string> = {
  ACTIVE: "Aktywny",
  SUSPENDED: "Zawieszony",
  DISABLED: "Wylaczony",
};

function nextMsgId() {
  return `CHAPS-GUI-${Date.now().toString().slice(-8)}`;
}

function StatusPill({ value, label }: { value: string; label: string }) {
  return <span className={`pill ${value.toLowerCase()}`}>{label}</span>;
}

export default function App() {
  const [view, setView] = useState<ViewMode>("bank");
  const [participants, setParticipants] = useState<Participant[]>([]);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [limits, setLimits] = useState<Limits | null>(null);
  const [bankLimits, setBankLimits] = useState<Limits | null>(null);
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [position, setPosition] = useState<Position | null>(null);
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [message, setMessage] = useState("Gotowe");
  const [busy, setBusy] = useState(false);
  const [selectedBank, setSelectedBank] = useState("BARCGB2L");

  const [paymentForm, setPaymentForm] = useState({
    msg_id: nextMsgId(),
    sender_bic: "BARCGB2L",
    receiver_bic: "HSBCGB44",
    amount: 250000,
  });
  const [participantForm, setParticipantForm] = useState({ bic: "", name: "", balance: 100000 });
  const [opsForm, setOpsForm] = useState({
    bic: "BARCGB2L",
    amount: 50000,
    status: "ACTIVE" as Participant["status"],
    reason: "FRAUD_SUSPECTED",
  });
  const [paymentOps, setPaymentOps] = useState({ msgId: "", remittanceInfo: "Zaktualizowany opis przelewu" });

  const selectedParticipant = participants.find((participant) => participant.bic === selectedBank);
  const bankPayments = payments.filter(
    (payment) => payment.sender_bic === selectedBank || payment.receiver_bic === selectedBank,
  );

  const totals = useMemo(() => {
    const settled = payments.filter((payment) => payment.status === "SETTLED").length;
    const queued = payments.filter((payment) => payment.status === "QUEUED" || payment.status === "PENDING").length;
    const rejected = payments.filter((payment) => payment.status === "REJECTED").length;
    return { settled, queued, rejected };
  }, [payments]);

  const bankTotals = useMemo(() => {
    const outgoing = bankPayments.filter((payment) => payment.sender_bic === selectedBank).length;
    const incoming = bankPayments.filter((payment) => payment.receiver_bic === selectedBank).length;
    const queued = bankPayments.filter((payment) => payment.status === "QUEUED" || payment.status === "PENDING").length;
    return { outgoing, incoming, queued };
  }, [bankPayments, selectedBank]);

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
    if (!participantData.some((participant) => participant.bic === selectedBank) && participantData[0]) {
      setSelectedBank(participantData[0].bic);
    }
    if (!paymentOps.msgId && paymentData[0]) {
      setPaymentOps((current) => ({ ...current, msgId: paymentData[0].msg_id }));
    }
  }

  async function refreshBankContext(bic: string) {
    const [positionData, limitData] = await Promise.all([getPosition(bic), getLimits(bic)]);
    setPosition(positionData);
    setBankLimits(limitData);
  }

  useEffect(() => {
    refresh().catch((error) => setMessage(error.message));
  }, []);

  useEffect(() => {
    if (selectedBank) {
      setPaymentForm((current) => ({ ...current, sender_bic: selectedBank }));
      refreshBankContext(selectedBank).catch((error) => setMessage(error.message));
    }
  }, [selectedBank]);

  async function run(label: string, action: () => Promise<unknown>, shouldRefresh = true) {
    setBusy(true);
    setMessage(`${label}...`);
    try {
      const result = await action();
      setMessage(`${label}: wykonano`);
      if (shouldRefresh) {
        await refresh();
        await refreshBankContext(selectedBank);
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
    run("Utworzenie platnosci CHAPS", async () => {
      const result = await createPayment(paymentForm);
      setPaymentOps((current) => ({ ...current, msgId: paymentForm.msg_id }));
      setPaymentForm((current) => ({ ...current, msg_id: nextMsgId() }));
      return result;
    });
  }

  function submitValidation() {
    run(
      "Walidacja platnosci",
      async () => {
        const result = await validatePayment(paymentForm);
        setValidation(result);
        return result;
      },
      false,
    );
  }

  function submitParticipant(event: FormEvent) {
    event.preventDefault();
    run("Rejestracja uczestnika", () => registerParticipant(participantForm));
  }

  return (
    <main className="shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Serwis platniczy</p>
          <h1>{serviceName}</h1>
        </div>
        <div className="top-actions">
          <div className="segmented" aria-label="Widok">
            <button className={view === "bank" ? "active-tab" : ""} onClick={() => setView("bank")} type="button">
              Bank
            </button>
            <button
              className={view === "operator" ? "active-tab" : ""}
              onClick={() => setView("operator")}
              type="button"
            >
              Operator
            </button>
          </div>
          <div className="status-strip">
            <span className={busy ? "dot busy" : "dot"} />
            <strong>{message}</strong>
          </div>
        </div>
      </section>

      {view === "bank" ? (
        <BankPage
          bankLimits={bankLimits}
          bankPayments={bankPayments}
          bankTotals={bankTotals}
          busy={busy}
          participant={selectedParticipant}
          participants={participants}
          paymentForm={paymentForm}
          position={position}
          selectedBank={selectedBank}
          setPaymentForm={setPaymentForm}
          setSelectedBank={setSelectedBank}
          submitPayment={submitPayment}
          submitValidation={submitValidation}
          validation={validation}
        />
      ) : (
        <OperatorPage
          busy={busy}
          limits={limits}
          opsForm={opsForm}
          participantForm={participantForm}
          participants={participants}
          paymentOps={paymentOps}
          payments={payments}
          position={position}
          run={run}
          schedule={schedule}
          setOpsForm={setOpsForm}
          setParticipantForm={setParticipantForm}
          setPaymentOps={setPaymentOps}
          setPosition={setPosition}
          submitParticipant={submitParticipant}
          totals={totals}
        />
      )}
    </main>
  );
}

function BankPage({
  bankLimits,
  bankPayments,
  bankTotals,
  busy,
  participant,
  participants,
  paymentForm,
  position,
  selectedBank,
  setPaymentForm,
  setSelectedBank,
  submitPayment,
  submitValidation,
  validation,
}: {
  bankLimits: Limits | null;
  bankPayments: Payment[];
  bankTotals: { outgoing: number; incoming: number; queued: number };
  busy: boolean;
  participant?: Participant;
  participants: Participant[];
  paymentForm: { msg_id: string; sender_bic: string; receiver_bic: string; amount: number };
  position: Position | null;
  selectedBank: string;
  setPaymentForm: (value: { msg_id: string; sender_bic: string; receiver_bic: string; amount: number }) => void;
  setSelectedBank: (bic: string) => void;
  submitPayment: (event: FormEvent) => void;
  submitValidation: () => void;
  validation: ValidationResult | null;
}) {
  return (
    <>
      <section className="page-intro">
        <div>
          <p className="section-label">Strona banku</p>
          <h2>Platnosci, walidacja i wlasna plynnosc</h2>
        </div>
        <label className="bank-picker">
          Bank
          <select value={selectedBank} onChange={(event) => setSelectedBank(event.target.value)}>
            {participants.map((item) => (
              <option key={item.bic} value={item.bic}>
                {item.bic} - {item.name}
              </option>
            ))}
          </select>
        </label>
      </section>

      <section className="metrics-grid">
        <article className="metric-card">
          <span>Saldo banku</span>
          <strong>{participant ? money.format(participant.balance) : "-"}</strong>
        </article>
        <article className="metric-card">
          <span>Dostepne srodki</span>
          <strong>{position ? money.format(position.available) : "-"}</strong>
        </article>
        <article className="metric-card">
          <span>Wychodzace</span>
          <strong>{bankTotals.outgoing}</strong>
        </article>
        <article className="metric-card">
          <span>W kolejce</span>
          <strong>{bankTotals.queued}</strong>
        </article>
      </section>

      <section className="work-grid">
        <form className="panel" onSubmit={submitPayment}>
          <div className="panel-head">
            <div>
              <p className="section-label">Bank / POST /v1/payments/chaps</p>
              <h2>Nowa platnosc banku</h2>
            </div>
            <button type="button" className="secondary-button" onClick={submitValidation}>
              Waliduj
            </button>
          </div>
          <div className="form-grid">
            <label>
              Id wiadomosci
              <input value={paymentForm.msg_id} onChange={(e) => setPaymentForm({ ...paymentForm, msg_id: e.target.value })} />
            </label>
            <label>
              BIC nadawcy
              <input
                value={paymentForm.sender_bic}
                onChange={(e) => setPaymentForm({ ...paymentForm, sender_bic: e.target.value.toUpperCase() })}
              />
            </label>
            <label>
              BIC odbiorcy
              <input
                value={paymentForm.receiver_bic}
                onChange={(e) => setPaymentForm({ ...paymentForm, receiver_bic: e.target.value.toUpperCase() })}
              />
            </label>
            <label>
              Kwota
              <input
                type="number"
                value={paymentForm.amount}
                onChange={(e) => setPaymentForm({ ...paymentForm, amount: Number(e.target.value) })}
              />
            </label>
          </div>
          {validation && (
            <div className={validation.valid ? "notice ok" : "notice bad"}>
              {validation.valid ? "Walidacja zakonczona powodzeniem" : validation.errors.join(", ")}. Dostepne srodki:{" "}
              {money.format(validation.available)}
            </div>
          )}
          <button className="primary-button" disabled={busy}>
            Wyslij platnosc
          </button>
        </form>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Bank / pozycja</p>
              <h2>Plynnosc i limity</h2>
            </div>
          </div>
          <dl className="detail-list">
            <div>
              <dt>BIC</dt>
              <dd>{selectedBank}</dd>
            </div>
            <div>
              <dt>Status</dt>
              <dd>{participant ? participantStatusLabels[participant.status] : "-"}</dd>
            </div>
            <div>
              <dt>Limit pojedynczej platnosci</dt>
              <dd>{bankLimits ? money.format(bankLimits.single_payment_limit) : "-"}</dd>
            </div>
            <div>
              <dt>Dostepna plynnosc intraday</dt>
              <dd>{bankLimits ? money.format(bankLimits.remaining_intraday_liquidity) : "-"}</dd>
            </div>
          </dl>
        </article>
      </section>

      <section className="panel">
        <div className="panel-head">
          <div>
            <p className="section-label">Bank / GET /v1/payments/chaps</p>
            <h2>Platnosci banku</h2>
          </div>
        </div>
        <PaymentTable payments={bankPayments} />
      </section>
    </>
  );
}

function OperatorPage({
  busy,
  limits,
  opsForm,
  participantForm,
  participants,
  paymentOps,
  payments,
  position,
  run,
  schedule,
  setOpsForm,
  setParticipantForm,
  setPaymentOps,
  setPosition,
  submitParticipant,
  totals,
}: {
  busy: boolean;
  limits: Limits | null;
  opsForm: { bic: string; amount: number; status: Participant["status"]; reason: string };
  participantForm: { bic: string; name: string; balance: number };
  participants: Participant[];
  paymentOps: { msgId: string; remittanceInfo: string };
  payments: Payment[];
  position: Position | null;
  run: (label: string, action: () => Promise<unknown>, shouldRefresh?: boolean) => Promise<unknown>;
  schedule: Schedule | null;
  setOpsForm: (value: { bic: string; amount: number; status: Participant["status"]; reason: string }) => void;
  setParticipantForm: (value: { bic: string; name: string; balance: number }) => void;
  setPaymentOps: (value: { msgId: string; remittanceInfo: string }) => void;
  setPosition: (value: Position) => void;
  submitParticipant: (event: FormEvent) => void;
  totals: { settled: number; queued: number; rejected: number };
}) {
  return (
    <>
      <section className="page-intro">
        <div>
          <p className="section-label">Strona operatora</p>
          <h2>Uczestnicy, limity, blokady i kolejka systemowa</h2>
        </div>
        {schedule && (
          <div className="compact-schedule">
            {schedule.date}: {schedule.opening_time}-{schedule.interbank_cutoff} {schedule.timezone}
          </div>
        )}
      </section>

      <section className="metrics-grid">
        <article className="metric-card">
          <span>Rozliczone</span>
          <strong>{totals.settled}</strong>
        </article>
        <article className="metric-card">
          <span>W kolejce / oczekujace</span>
          <strong>{totals.queued}</strong>
        </article>
        <article className="metric-card">
          <span>Odrzucone</span>
          <strong>{totals.rejected}</strong>
        </article>
        <article className="metric-card">
          <span>Plynnosc systemu</span>
          <strong>{limits ? money.format(limits.total_available_liquidity) : "-"}</strong>
        </article>
      </section>

      <section className="work-grid">
        <form className="panel" onSubmit={submitParticipant}>
          <div className="panel-head">
            <div>
              <p className="section-label">Operator / POST /v1/participants/register</p>
              <h2>Rejestracja uczestnika</h2>
            </div>
          </div>
          <div className="form-grid">
            <label>
              BIC
              <input value={participantForm.bic} onChange={(e) => setParticipantForm({ ...participantForm, bic: e.target.value.toUpperCase() })} />
            </label>
            <label>
              Nazwa
              <input value={participantForm.name} onChange={(e) => setParticipantForm({ ...participantForm, name: e.target.value })} />
            </label>
            <label>
              Saldo poczatkowe
              <input
                type="number"
                value={participantForm.balance}
                onChange={(e) => setParticipantForm({ ...participantForm, balance: Number(e.target.value) })}
              />
            </label>
          </div>
          <button className="primary-button" disabled={busy}>
            Zarejestruj bank
          </button>
        </form>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Operator / kontrola uczestnika</p>
              <h2>Status, blokada i plynnosc</h2>
            </div>
          </div>
          <div className="form-grid">
            <label>
              BIC
              <input value={opsForm.bic} onChange={(e) => setOpsForm({ ...opsForm, bic: e.target.value.toUpperCase() })} />
            </label>
            <label>
              Status
              <select value={opsForm.status} onChange={(e) => setOpsForm({ ...opsForm, status: e.target.value as Participant["status"] })}>
                <option value="ACTIVE">Aktywny</option>
                <option value="SUSPENDED">Zawieszony</option>
                <option value="DISABLED">Wylaczony</option>
              </select>
            </label>
            <label>
              Powod
              <input value={opsForm.reason} onChange={(e) => setOpsForm({ ...opsForm, reason: e.target.value })} />
            </label>
            <label>
              Zasilenie
              <input type="number" value={opsForm.amount} onChange={(e) => setOpsForm({ ...opsForm, amount: Number(e.target.value) })} />
            </label>
          </div>
          <div className="button-row">
            <button className="secondary-button" type="button" onClick={() => run("Zmiana statusu", () => updateParticipantStatus(opsForm.bic, opsForm.status, opsForm.reason))}>
              Ustaw status
            </button>
            <button className="secondary-button" type="button" onClick={() => run("Blokada uczestnika", () => blockParticipant(opsForm.bic, opsForm.reason))}>
              Zablokuj
            </button>
            <button className="secondary-button" type="button" onClick={() => run("Odblokowanie uczestnika", () => unblockParticipant(opsForm.bic))}>
              Odblokuj
            </button>
            <button className="secondary-button" type="button" onClick={() => run("Zasilenie plynnosci", () => topUpLiquidity({ bic: opsForm.bic, amount: opsForm.amount }))}>
              Zasil
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={() =>
                run(
                  "Pobranie pozycji",
                  async () => {
                    const result = await getPosition(opsForm.bic);
                    setPosition(result);
                    return result;
                  },
                  false,
                )
              }
            >
              Pozycja
            </button>
            <button className="secondary-button" type="button" onClick={() => run("Szczegoly blokady", () => getBlockDetails(opsForm.bic), false)}>
              Szczegoly blokady
            </button>
          </div>
          {position && <div className="notice">Dostepne srodki dla {position.bic}: {money.format(position.available)}</div>}
        </article>
      </section>

      <section className="work-grid">
        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Operator / kontrola platnosci</p>
              <h2>Autoryzacja, korekta, anulowanie</h2>
            </div>
          </div>
          <div className="form-grid">
            <label>
              Id wiadomosci
              <input value={paymentOps.msgId} onChange={(e) => setPaymentOps({ ...paymentOps, msgId: e.target.value })} />
            </label>
            <label>
              Opis przelewu
              <input value={paymentOps.remittanceInfo} onChange={(e) => setPaymentOps({ ...paymentOps, remittanceInfo: e.target.value })} />
            </label>
          </div>
          <div className="button-row">
            <button className="secondary-button" type="button" onClick={() => run("Autoryzacja platnosci", () => authorizePayment(paymentOps.msgId))}>
              Autoryzuj
            </button>
            <button className="secondary-button" type="button" onClick={() => run("Korekta platnosci", () => amendPayment(paymentOps.msgId, paymentOps.remittanceInfo))}>
              Koryguj
            </button>
            <button className="danger-button" type="button" onClick={() => run("Anulowanie platnosci", () => cancelPayment(paymentOps.msgId))}>
              Anuluj
            </button>
          </div>
        </article>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Operator / limity</p>
              <h2>Parametry systemu</h2>
            </div>
          </div>
          <dl className="detail-list">
            <div>
              <dt>Limit pojedynczej platnosci</dt>
              <dd>{limits ? money.format(limits.single_payment_limit) : "-"}</dd>
            </div>
            <div>
              <dt>Dzienny limit uczestnika</dt>
              <dd>{limits ? money.format(limits.daily_participant_limit) : "-"}</dd>
            </div>
            <div>
              <dt>Plynnosc systemu</dt>
              <dd>{limits ? money.format(limits.total_available_liquidity) : "-"}</dd>
            </div>
          </dl>
        </article>
      </section>

      <section className="table-grid">
        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Operator / GET /v1/participants</p>
              <h2>Uczestnicy</h2>
            </div>
          </div>
          <ParticipantTable participants={participants} />
        </article>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Operator / GET /v1/payments/chaps</p>
              <h2>Platnosci</h2>
            </div>
          </div>
          <PaymentTable payments={payments} onSelect={(msgId) => setPaymentOps({ ...paymentOps, msgId })} />
        </article>
      </section>
    </>
  );
}

function ParticipantTable({ participants }: { participants: Participant[] }) {
  return (
    <table>
      <thead>
        <tr>
          <th>BIC</th>
          <th>Nazwa</th>
          <th>Status</th>
          <th>Saldo</th>
        </tr>
      </thead>
      <tbody>
        {participants.map((participant) => (
          <tr key={participant.bic}>
            <td>{participant.bic}</td>
            <td>{participant.name}</td>
            <td>
              <StatusPill value={participant.status} label={participantStatusLabels[participant.status]} />
            </td>
            <td>{money.format(participant.balance)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function PaymentTable({ payments, onSelect }: { payments: Payment[]; onSelect?: (msgId: string) => void }) {
  return (
    <table>
      <thead>
        <tr>
          <th>Id wiadomosci</th>
          <th>Trasa</th>
          <th>Kwota</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {payments.map((payment) => (
          <tr key={payment.msg_id} onClick={() => onSelect?.(payment.msg_id)}>
            <td>{payment.msg_id}</td>
            <td>
              {payment.sender_bic} &gt; {payment.receiver_bic}
            </td>
            <td>{money.format(payment.amount)}</td>
            <td>
              <StatusPill value={payment.status} label={paymentStatusLabels[payment.status]} />
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
