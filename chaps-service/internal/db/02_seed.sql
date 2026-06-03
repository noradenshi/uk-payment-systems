-- Clean up any existing test data
TRUNCATE participant_liquidity, participant_statuses, participant_profiles CASCADE;

-- Seed Member Banks across the normalized schema
WITH seed_data (bic, name, sort_code, bal) AS (
    VALUES 
        ('BARCGB2L', 'Barclays Bank', '20-00-00', 1000000.00),
        ('HSBCGB44', 'HSBC UK', '40-00-00', 500000.00),
        ('LLOYGB21', 'Lloyds Bank', '30-00-00', 750000.00),
        ('SNDRUK22', 'Alice Bank', '60-00-00', 1000000.00)
),
ins_profiles AS (
    INSERT INTO participant_profiles (bic_code, name, sort_code)
    SELECT bic, name, sort_code FROM seed_data
    RETURNING bic_code
),
ins_statuses AS (
    INSERT INTO participant_statuses (bic_code, status)
    SELECT bic_code, 'ACTIVE' FROM ins_profiles
)
INSERT INTO participant_liquidity (bic_code, balance)
SELECT sd.bic, sd.bal 
FROM seed_data sd;
