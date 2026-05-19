CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE participant_profiles (
    bic_code VARCHAR(11) PRIMARY KEY,
    name TEXT NOT NULL,
    currency VARCHAR(3) DEFAULT 'GBP',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE participant_liquidity (
    bic_code VARCHAR(11) PRIMARY KEY REFERENCES participant_profiles(bic_code),
    balance DECIMAL(20, 2) NOT NULL DEFAULT 0.00,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE participant_status AS ENUM ('ACTIVE', 'SUSPENDED', 'DISABLED');

CREATE TABLE participant_statuses (
    bic_code VARCHAR(11) PRIMARY KEY REFERENCES participant_profiles(bic_code),
    status participant_status DEFAULT 'ACTIVE',
    is_closed BOOLEAN DEFAULT FALSE,
    blocked_at TIMESTAMP WITH TIME ZONE,
    block_reason TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE payment_status AS ENUM ('PENDING', 'QUEUED', 'SETTLED', 'REJECTED');

CREATE TABLE transactions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    msg_id VARCHAR(35) UNIQUE NOT NULL,
    end_to_end_id VARCHAR(35),
    sender_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    receiver_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    status payment_status DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE journal_entries (
    id SERIAL PRIMARY KEY,
    transaction_id UUID REFERENCES transactions(id) ON DELETE RESTRICT,
    account_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    entry_date TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_transactions_msg_id ON transactions(msg_id);
CREATE INDEX idx_transactions_sender ON transactions(sender_bic);
CREATE INDEX idx_transactions_status ON transactions(status);
CREATE INDEX idx_journal_transaction_id ON journal_entries(transaction_id);
CREATE INDEX idx_journal_account ON journal_entries(account_bic);

CREATE OR REPLACE FUNCTION notify_liquidity_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('liquidity_event', NEW.account_bic);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_liquidity_change
AFTER INSERT ON journal_entries
FOR EACH ROW
WHEN (NEW.amount > 0)
EXECUTE FUNCTION notify_liquidity_change();
