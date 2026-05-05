import { useEffect, useState } from "react";
import { getDashboardData, samplePaymentXml, sendDemoPayment } from "./api";
import type { DashboardData, QueueItem, ServiceItem } from "./types";

const emptyState: DashboardData = {
  metrics: [],
  accounts: [],
  queue: [],
  services: [],
};

const statusLabels: Record<string, string> = {
  SETTLED: "Rozliczona",
  QUEUED: "W kolejce",
  REJECTED: "Odrzucona",
  PENDING: "Oczekująca",
  online: "Online",
  degraded: "Ograniczona",
  offline: "Offline",
};

function QueueStatus({ status }: { status: QueueItem["status"] | ServiceItem["state"] }) {
  const label = statusLabels[status] ?? status;
  return <span className={`pill pill-${status.toLowerCase()}`}>{label}</span>;
}

export default function App() {
  const [data, setData] = useState<DashboardData>(emptyState);
  const [loading, setLoading] = useState(true);
  const [demoResult, setDemoResult] = useState("Jeszcze nie wysłano");

  useEffect(() => {
    getDashboardData()
      .then(setData)
      .finally(() => setLoading(false));
  }, []);

  async function handleSendDemo() {
    setDemoResult("Wysyłanie...");
    const result = await sendDemoPayment(samplePaymentXml);
    setDemoResult(result);
  }

  return (
    <main className="shell">
      <section className="hero">
        <div>
          <p className="eyebrow">Moduł CHAPS</p>
          <h1>Panel operatora CHAPS</h1>
          <p className="lead">
            Prosty panel do podglądu uczestników, kolejki rozliczeń i stanu serwisu CHAPS.
          </p>
        </div>
        <div className="hero-box">
          <button className="primary-button" onClick={handleSendDemo}>
            Wyślij płatność demo
          </button>
          <div className="demo-box">
            <span>Wynik z bramy</span>
            <strong>{demoResult}</strong>
          </div>
        </div>
      </section>

      <section className="metrics-grid">
        {loading
          ? Array.from({ length: 4 }).map((_, index) => <div className="metric-card" key={index} />)
          : data.metrics.map((metric) => (
              <article className="metric-card metric-ready" key={metric.label}>
                <span>{metric.label}</span>
                <strong>{metric.value}</strong>
              </article>
            ))}
      </section>

      <section className="content-grid">
        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Konta</p>
              <h2>Uczestnicy CHAPS</h2>
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>BIC</th>
                <th>Nazwa</th>
                <th>Saldo</th>
              </tr>
            </thead>
            <tbody>
              {data.accounts.map((account) => (
                <tr key={account.bic}>
                  <td>{account.bic}</td>
                  <td>{account.name}</td>
                  <td>{account.balance}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </article>

        <article className="panel">
          <div className="panel-head">
            <div>
              <p className="section-label">Usługi</p>
              <h2>Stan systemu</h2>
            </div>
          </div>
          <div className="service-list">
            {data.services.map((service) => (
              <div className="service-item" key={service.label}>
                <div>
                  <strong>{service.label}</strong>
                  <p>{service.detail}</p>
                </div>
                <QueueStatus status={service.state} />
              </div>
            ))}
          </div>
        </article>
      </section>

      <section className="panel">
        <div className="panel-head">
          <div>
            <p className="section-label">Kolejka</p>
            <h2>Ostatnie płatności CHAPS</h2>
          </div>
        </div>
        <div className="queue-list">
          {data.queue.map((item) => (
            <article className="queue-item" key={item.msgId}>
              <div>
                <strong>{item.msgId}</strong>
                <p>
                  {item.sender} do {item.receiver} · {item.amount}
                </p>
              </div>
              <div className="queue-meta">
                <QueueStatus status={item.status} />
                <span>{item.time}</span>
              </div>
            </article>
          ))}
        </div>
      </section>
    </main>
  );
}
