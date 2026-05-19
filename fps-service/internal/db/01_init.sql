CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TYPE participant_status AS ENUM ('ACTIVE', 'SUSPENDED', 'DISABLED');
CREATE TYPE payment_status AS ENUM ('PENDING', 'QUEUED', 'SETTLED', 'REJECTED');

CREATE TABLE participant_profiles (
    bic_code VARCHAR(11) PRIMARY KEY,
    name TEXT NOT NULL,
    currency VARCHAR(3) DEFAULT 'GBP',
    participant_type VARCHAR(10) NOT NULL DEFAULT 'DIRECT' CHECK (participant_type IN ('DIRECT', 'INDIRECT')),
    sponsor_bic VARCHAR(11),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE participant_liquidity (
    bic_code VARCHAR(11) PRIMARY KEY REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    balance DECIMAL(20, 2) NOT NULL DEFAULT 0.00,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE participant_statuses (
    bic_code VARCHAR(11) PRIMARY KEY REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    status participant_status DEFAULT 'ACTIVE',
    is_closed BOOLEAN DEFAULT FALSE,
    blocked_at TIMESTAMP WITH TIME ZONE,
    block_reason TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE fps_transactions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    msg_id VARCHAR(35) UNIQUE NOT NULL,
    end_to_end_id VARCHAR(35),
    sender_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    receiver_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    status payment_status DEFAULT 'PENDING',
    payment_type VARCHAR(20) DEFAULT 'SIP',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE fps_forward_dated (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    msg_id VARCHAR(35) UNIQUE NOT NULL,
    sender_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    receiver_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    execution_date DATE NOT NULL,
    status VARCHAR(20) DEFAULT 'SCHEDULED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE fps_standing_orders (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    reference VARCHAR(35) UNIQUE NOT NULL,
    sender_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    receiver_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    frequency VARCHAR(10) NOT NULL,
    next_date DATE NOT NULL,
    end_date DATE,
    status VARCHAR(20) DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE fps_bulk_submissions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    filename VARCHAR(255) NOT NULL,
    sender_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    total_items INT NOT NULL,
    total_value DECIMAL(20, 2) NOT NULL,
    status VARCHAR(20) DEFAULT 'RECEIVED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE fps_dns_cycles (
    id SERIAL PRIMARY KEY,
    cycle_start TIMESTAMPTZ NOT NULL,
    cycle_end TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ,
    status VARCHAR(20) DEFAULT 'OPEN'
);

CREATE TABLE fps_journal_entries (
    id SERIAL PRIMARY KEY,
    transaction_id UUID REFERENCES fps_transactions(id) ON DELETE RESTRICT,
    account_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    entry_date TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_fps_transactions_msg_id ON fps_transactions(msg_id);
CREATE INDEX idx_fps_transactions_sender ON fps_transactions(sender_bic);
CREATE INDEX idx_fps_transactions_status ON fps_transactions(status);
CREATE INDEX idx_fps_journal_transaction_id ON fps_journal_entries(transaction_id);
CREATE INDEX idx_fps_journal_account ON fps_journal_entries(account_bic);
