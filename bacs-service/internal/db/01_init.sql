CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE participant_profiles (
    bic_code VARCHAR(11) PRIMARY KEY,
    name TEXT NOT NULL,
    currency VARCHAR(3) DEFAULT 'GBP',
    su_code VARCHAR(12),
    is_service_user BOOLEAN DEFAULT FALSE,
    is_destination_user BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE participant_liquidity (
    bic_code VARCHAR(11) PRIMARY KEY REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    balance DECIMAL(20, 2) NOT NULL DEFAULT 0.00,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE participant_status AS ENUM ('ACTIVE', 'SUSPENDED', 'DISABLED');
CREATE TABLE participant_statuses (
    bic_code VARCHAR(11) PRIMARY KEY REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    status participant_status DEFAULT 'ACTIVE',
    is_closed BOOLEAN DEFAULT FALSE,
    overdraft_limit DECIMAL(20, 2) NOT NULL DEFAULT 30000000.00,
    liquidity_breach_at TIMESTAMP WITH TIME ZONE,
    blocked_at TIMESTAMP WITH TIME ZONE,
    block_reason TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE cycle_status AS ENUM ('OPEN', 'PROCESSING', 'AWAITING_SETTLEMENT', 'SETTLED');
CREATE TABLE bacs_cycles (
    id SERIAL PRIMARY KEY,
    input_date DATE NOT NULL,
    processing_date DATE NOT NULL,
    settlement_date DATE NOT NULL,
    status cycle_status DEFAULT 'OPEN',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE submission_status AS ENUM ('RECEIVED', 'VALIDATED', 'PROCESSING', 'ACCEPTED', 'REJECTED', 'RECALLED');
CREATE TABLE bacs_submissions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    filename VARCHAR(255) NOT NULL,
    su_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    total_volume INT NOT NULL DEFAULT 0,
    total_value DECIMAL(20, 2) NOT NULL DEFAULT 0.00,
    status submission_status DEFAULT 'RECEIVED',
    cycle_id INT REFERENCES bacs_cycles(id),
    error_count INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TYPE bacs_record_type AS ENUM ('DIRECT_DEBIT', 'DIRECT_CREDIT');
CREATE TABLE bacs_transactions (
    id SERIAL PRIMARY KEY,
    submission_id UUID REFERENCES bacs_submissions(id) ON DELETE RESTRICT,
    record_type bacs_record_type NOT NULL,
    volume_header_no INT NOT NULL DEFAULT 1,
    dest_sort_code VARCHAR(9) NOT NULL,
    dest_account VARCHAR(9) NOT NULL,
    debtor_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    creditor_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    originator_ref VARCHAR(15),
    reference VARCHAR(14),
    su_code VARCHAR(13),
    status VARCHAR(20) DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE bacs_bilateral_positions (
    id SERIAL PRIMARY KEY,
    cycle_id INT REFERENCES bacs_cycles(id) ON DELETE RESTRICT,
    debtor_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    creditor_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    gross_amount DECIMAL(20, 2) NOT NULL,
    net_amount DECIMAL(20, 2) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (cycle_id, debtor_bic, creditor_bic)
);

CREATE TABLE bacs_net_positions (
    id SERIAL PRIMARY KEY,
    cycle_id INT REFERENCES bacs_cycles(id) ON DELETE RESTRICT,
    bic_code VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    net_position DECIMAL(20, 2) NOT NULL,
    balance_before DECIMAL(20, 2) NOT NULL,
    overdraft_limit DECIMAL(20, 2) NOT NULL,
    status VARCHAR(20) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE (cycle_id, bic_code)
);

CREATE TABLE bacs_mandates (
    id SERIAL PRIMARY KEY,
    reference VARCHAR(35) NOT NULL,
    su_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    payer_name TEXT,
    payer_sort_code VARCHAR(9),
    payer_account VARCHAR(9),
    amount DECIMAL(20, 2),
    frequency VARCHAR(10) DEFAULT 'MONTHLY',
    status VARCHAR(20) DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE bacs_returns (
    id SERIAL PRIMARY KEY,
    original_transaction_id INT REFERENCES bacs_transactions(id) ON DELETE RESTRICT,
    reason_code VARCHAR(10) NOT NULL,
    amount DECIMAL(20, 2) NOT NULL,
    return_date TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE bacs_journal_entries (
    id SERIAL PRIMARY KEY,
    submission_id UUID REFERENCES bacs_submissions(id) ON DELETE RESTRICT,
    account_bic VARCHAR(11) REFERENCES participant_profiles(bic_code) ON DELETE RESTRICT,
    amount DECIMAL(20, 2) NOT NULL,
    entry_date TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_bacs_submissions_su ON bacs_submissions(su_bic);
CREATE INDEX idx_bacs_submissions_cycle ON bacs_submissions(cycle_id);
CREATE INDEX idx_bacs_transactions_submission ON bacs_transactions(submission_id);
CREATE INDEX idx_bacs_transactions_bilateral ON bacs_transactions(debtor_bic, creditor_bic);
CREATE INDEX idx_bacs_bilateral_cycle ON bacs_bilateral_positions(cycle_id);
CREATE INDEX idx_bacs_net_positions_cycle ON bacs_net_positions(cycle_id);
CREATE INDEX idx_bacs_mandates_su ON bacs_mandates(su_bic);
