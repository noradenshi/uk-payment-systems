-- Clean up any existing test data
TRUNCATE participant_liquidity, participant_statuses, participant_profiles CASCADE;

-- Seed Member Banks across the normalized schema
WITH seed_data (bic, name, bal) AS (
    VALUES 
        ('BARCGB2L', 'Barclays Bank', 1000000.00),
        ('HSBCGB44', 'HSBC UK', 500000.00),
        ('LLOYGB21', 'Lloyds Bank', 750000.00),
        ('SNDRUK22', 'Alice Bank', 1000000.00)
),
ins_profiles AS (
    INSERT INTO participant_profiles (bic_code, name)
    SELECT bic, name FROM seed_data
    RETURNING bic_code
),
ins_statuses AS (
    INSERT INTO participant_statuses (bic_code, status)
    SELECT bic_code, 'ACTIVE' FROM ins_profiles
)
INSERT INTO participant_liquidity (bic_code, balance)
SELECT sd.bic, sd.bal 
FROM seed_data sd;
