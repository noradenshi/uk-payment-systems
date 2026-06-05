DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'uq_participant_name'
  ) THEN
    ALTER TABLE participant_profiles ADD CONSTRAINT uq_participant_name UNIQUE (name);
  END IF;
END $$;
