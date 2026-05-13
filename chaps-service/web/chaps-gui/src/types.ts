export type Participant = {
  bic: string;
  name: string;
  status: "ACTIVE" | "SUSPENDED" | "DISABLED";
  balance: number;
  currency: string;
  is_closed: boolean;
  block_reason?: string;
};

export type Payment = {
  msg_id: string;
  sender_bic: string;
  receiver_bic: string;
  amount: number;
  status: "PENDING" | "QUEUED" | "SETTLED" | "REJECTED";
  created_at: string;
};

export type Position = {
  bic: string;
  balance: number;
  earmarked: number;
  available: number;
};

export type Limits = {
  currency: string;
  single_payment_limit: number;
  daily_participant_limit: number;
  total_available_liquidity: number;
  remaining_intraday_liquidity: number;
};

export type Schedule = {
  date: string;
  opening_time: string;
  customer_cutoff: string;
  interbank_cutoff: string;
  timezone: string;
};

export type ValidationResult = {
  valid: boolean;
  checks: string[];
  errors: string[];
  available: number;
};
