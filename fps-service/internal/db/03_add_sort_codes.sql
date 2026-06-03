ALTER TABLE participant_profiles ADD COLUMN IF NOT EXISTS sort_code VARCHAR(9);
ALTER TABLE fps_transactions ADD COLUMN IF NOT EXISTS sender_sort_code VARCHAR(9);
ALTER TABLE fps_transactions ADD COLUMN IF NOT EXISTS receiver_sort_code VARCHAR(9);
