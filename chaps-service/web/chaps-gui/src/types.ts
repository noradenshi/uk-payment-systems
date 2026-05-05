export type Metric = {
  label: string;
  value: string;
};

export type QueueItem = {
  msgId: string;
  sender: string;
  receiver: string;
  amount: string;
  status: "SETTLED" | "QUEUED" | "REJECTED" | "PENDING";
  time: string;
};

export type AccountRow = {
  bic: string;
  name: string;
  balance: string;
};

export type ServiceItem = {
  label: string;
  state: "online" | "degraded" | "offline";
  detail: string;
};

export type DashboardData = {
  metrics: Metric[];
  accounts: AccountRow[];
  queue: QueueItem[];
  services: ServiceItem[];
};
