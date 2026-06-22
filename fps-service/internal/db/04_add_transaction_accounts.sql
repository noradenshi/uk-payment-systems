ALTER TABLE fps_transactions ADD COLUMN IF NOT EXISTS sender_account VARCHAR(34);
ALTER TABLE fps_transactions ADD COLUMN IF NOT EXISTS receiver_account VARCHAR(34);
