-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Core Tables
CREATE TABLE accounts (
    bic_code VARCHAR(11) PRIMARY KEY,
    name TEXT NOT NULL,
    balance DECIMAL(20, 2) NOT NULL DEFAULT 0.00,
    currency     VARCHAR(3) DEFAULT 'GBP',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE payment_status AS ENUM ('PENDING', 'QUEUED', 'SETTLED', 'REJECTED');

CREATE TABLE transactions (
    id UUID PRIMARY KEY DEFAULT uuidv7(), -- Native in Postgres 18
    msg_id VARCHAR(35) UNIQUE NOT NULL,
    sender_bic VARCHAR(11) REFERENCES accounts(bic_code),
    receiver_bic VARCHAR(11) REFERENCES accounts(bic_code),
    amount DECIMAL(20, 2) NOT NULL,
    status          payment_status DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE journal_entries (
    id SERIAL PRIMARY KEY,
    transaction_id UUID REFERENCES transactions(id),
    account_bic VARCHAR(11) REFERENCES accounts(bic_code),
    amount DECIMAL(20, 2) NOT NULL, -- Negative for Debit, Positive for Credit
    entry_date TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Trigger Function for Real-time Notification
CREATE OR REPLACE FUNCTION notify_liquidity_change()
RETURNS TRIGGER AS $$
BEGIN
    -- Notify on 'liquidity_event' channel with the BIC of the bank that received funds
    PERFORM pg_notify('liquidity_event', NEW.account_bic);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_liquidity_change
AFTER INSERT ON journal_entries
FOR EACH ROW
WHEN (NEW.amount > 0)
EXECUTE FUNCTION notify_liquidity_change();

-- Seed some Member Banks for testing
INSERT INTO accounts (bic_code, name, balance) VALUES 
('BARCGB2L', 'Barclays Bank', 1000000.00),
('HSBCGB44', 'HSBC UK', 500000.00),
('LLOYGB21', 'Lloyds Bank', 750000.00),
('SNDRUK22', 'Alice Bank', 1000000.00);
