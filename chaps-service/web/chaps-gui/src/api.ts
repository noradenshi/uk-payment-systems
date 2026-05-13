import type { Limits, Participant, Payment, Position, Schedule, ValidationResult } from "./types";

const jsonHeaders = { "Content-Type": "application/json" };

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  const contentType = response.headers.get("Content-Type") ?? "";
  const payload = contentType.includes("application/json") ? await response.json() : await response.text();

  if (!response.ok) {
    const message = typeof payload === "string" ? payload : payload.error ?? "Żądanie nie powiodło się";
    throw new Error(message);
  }
  return payload as T;
}

export function listParticipants() {
  return request<Participant[]>("/v1/participants");
}

export function registerParticipant(payload: { bic: string; name: string; balance: number }) {
  return request<{ bic: string; status: string }>("/v1/participants/register", {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify(payload),
  });
}

export function updateParticipantStatus(bic: string, status: Participant["status"], reason = "") {
  return request<{ bic: string; status: string }>(`/v1/participants/${bic}/status`, {
    method: "PATCH",
    headers: jsonHeaders,
    body: JSON.stringify({ status, reason }),
  });
}

export function blockParticipant(bic: string, reason: string) {
  return request<{ bic: string; status: string }>(`/v1/participants/${bic}/block`, {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify({ reason }),
  });
}

export function unblockParticipant(bic: string) {
  return request<{ bic: string; status: string }>(`/v1/participants/${bic}/block`, { method: "DELETE" });
}

export function getBlockDetails(bic: string) {
  return request<Record<string, unknown>>(`/v1/participants/${bic}/block`);
}

export function getPosition(bic: string) {
  return request<Position>(`/v1/participants/${bic}/positions`);
}

export function topUpLiquidity(payload: { bic: string; amount: number }) {
  return request<{ bic: string; status: string }>("/v1/liquidity/top-up", {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify(payload),
  });
}

export function listPayments() {
  return request<Payment[]>("/v1/payments/chaps?limit=25");
}

export function createPayment(payload: {
  msg_id: string;
  sender_bic: string;
  receiver_bic: string;
  amount: number;
}) {
  return request<{ msg_id: string; status: string; iso_status: string; reason_code: string }>("/v1/payments/chaps", {
    method: "POST",
    headers: {
      ...jsonHeaders,
      "Idempotency-Key": payload.msg_id,
      "X-Digital-Signature": "gui-simulation",
    },
    body: JSON.stringify(payload),
  });
}

export function validatePayment(payload: { sender_bic: string; receiver_bic: string; amount: number }) {
  return request<ValidationResult>("/v1/payments/chaps/validate", {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify(payload),
  });
}

export function authorizePayment(msgId: string) {
  return request<{ msg_id: string; status: string }>(`/v1/payments/chaps/${msgId}/authorize`, { method: "POST" });
}

export function cancelPayment(msgId: string) {
  return request<{ msg_id: string; status: string }>(`/v1/payments/chaps/${msgId}`, { method: "DELETE" });
}

export function amendPayment(msgId: string, remittanceInfo: string) {
  return request<{ msg_id: string; status: string }>(`/v1/payments/chaps/${msgId}/amend`, {
    method: "POST",
    headers: jsonHeaders,
    body: JSON.stringify({ remittance_info: remittanceInfo }),
  });
}

export function getLimits(bic = "") {
  return request<Limits>(`/v1/payments/chaps/limits${bic ? `?bic=${bic}` : ""}`);
}

export function getSchedule() {
  return request<Schedule>("/v1/system/schedule");
}
